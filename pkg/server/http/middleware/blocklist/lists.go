package blocklist

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	typeIP        = "ip"
	typeRDNS      = "rdns"
	typeUserAgent = "user_agent"
	typeURI       = "uri"

	maxListSize  = 64 << 20
	fetchTimeout = time.Minute
)

// ruleSet is a compiled set of rules.
type ruleSet struct {
	ips       []netip.Prefix
	rdns      []string
	userAgent []*regexp.Regexp
	uri       []*regexp.Regexp
}

func (s *ruleSet) empty() bool {
	return len(s.ips) == 0 && len(s.rdns) == 0 && len(s.userAgent) == 0 && len(s.uri) == 0
}

func (s *ruleSet) add(typ, entry string) error {
	switch typ {
	case typeIP:
		p, err := parsePrefix(entry)
		if err != nil {
			return err
		}
		s.ips = append(s.ips, p)
	case typeRDNS:
		d := normalizeDomain(entry)
		if d == "" {
			return fmt.Errorf("rdns %q is empty", entry)
		}
		s.rdns = append(s.rdns, d)
	case typeUserAgent, typeURI:
		re, err := regexp.Compile(entry)
		if err != nil {
			return err
		}
		if typ == typeURI {
			s.uri = append(s.uri, re)
		} else {
			s.userAgent = append(s.userAgent, re)
		}
	default:
		return fmt.Errorf("type %q is unknown, expected ip, rdns, user_agent or uri", typ)
	}

	return nil
}

func (s *ruleSet) merge(o *ruleSet) {
	s.ips = append(s.ips, o.ips...)
	s.rdns = append(s.rdns, o.rdns...)
	s.userAgent = append(s.userAgent, o.userAgent...)
	s.uri = append(s.uri, o.uri...)
}

// match reports whether r matches any rule; reverse DNS is looked up last and
// only when there are rdns rules.
func (s *ruleSet) match(ctx context.Context, r *http.Request, ip netip.Addr, dns *lookups, allow bool) bool {
	if ip.IsValid() {
		for _, p := range s.ips {
			if p.Contains(ip) {
				return true
			}
		}
	}

	if len(s.userAgent) > 0 {
		ua := r.UserAgent()
		for _, re := range s.userAgent {
			if re.MatchString(ua) {
				return true
			}
		}
	}

	if len(s.uri) > 0 {
		// upstreams may normalize the path, e.g. "/x/../wp-admin" or
		// "//wp-admin": deny matches either form, allow needs both
		raw := r.URL.Path
		clean := cleanPath(raw)
		for _, re := range s.uri {
			rawOK := re.MatchString(raw)
			cleanOK := rawOK
			if clean != raw {
				cleanOK = re.MatchString(clean)
			}

			if (allow && rawOK && cleanOK) || (!allow && (rawOK || cleanOK)) {
				return true
			}
		}
	}

	if len(s.rdns) > 0 && ip.IsValid() && dns != nil {
		for _, name := range dns.names(ctx, ip) {
			for _, suffix := range s.rdns {
				if name == suffix || strings.HasSuffix(name, "."+suffix) {
					return true
				}
			}
		}
	}

	return false
}

type compiled struct {
	allow ruleSet
	deny  ruleSet
}

type source struct {
	URL
	deny bool
	// last successfully loaded entries, kept when a refresh fails
	set *ruleSet
}

type lists struct {
	static  compiled
	sources []*source

	mu      sync.Mutex
	current atomic.Pointer[compiled]
	client  *http.Client
}

func newLists(allow, deny Rules) (*lists, error) {
	l := &lists{client: &http.Client{Timeout: fetchTimeout}}

	for _, side := range []struct {
		name  string
		rules Rules
		set   *ruleSet
		deny  bool
	}{
		{"allow", allow, &l.static.allow, false},
		{"deny", deny, &l.static.deny, true},
	} {
		for typ, entries := range map[string][]string{
			typeIP:        side.rules.IP,
			typeRDNS:      side.rules.RDNS,
			typeUserAgent: side.rules.UserAgent,
			typeURI:       side.rules.URI,
		} {
			for _, e := range entries {
				if err := side.set.add(typ, strings.TrimSpace(e)); err != nil {
					return nil, fmt.Errorf("blocklist %s %s: %w", side.name, typ, err)
				}
			}
		}

		for _, u := range side.rules.URLs {
			if u.URL == "" {
				return nil, fmt.Errorf("blocklist %s urls: url is empty", side.name)
			}

			u.Type = strings.ToLower(strings.TrimSpace(u.Type))
			switch u.Type {
			case typeIP, typeRDNS, typeUserAgent, typeURI:
			default:
				return nil, fmt.Errorf("blocklist %s urls %q: type %q is unknown, expected ip, rdns, user_agent or uri", side.name, u.URL, u.Type)
			}

			l.sources = append(l.sources, &source{URL: u, deny: side.deny})
		}
	}

	c := l.static
	l.current.Store(&c)

	return l, nil
}

func (l *lists) remote() bool {
	return len(l.sources) > 0
}

// rdns reports whether reverse DNS can be needed, also by remote lists.
func (l *lists) rdns() bool {
	if len(l.static.allow.rdns) > 0 || len(l.static.deny.rdns) > 0 {
		return true
	}

	for _, s := range l.sources {
		if s.Type == typeRDNS {
			return true
		}
	}

	return false
}

func (l *lists) run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			l.load(ctx)
		}
	}
}

// load fetches the remote lists and swaps in the new rules.
func (l *lists) load(ctx context.Context) {
	if !l.remote() {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	for _, s := range l.sources {
		set, err := l.fetch(ctx, s.URL)
		if err != nil {
			if ctx.Err() != nil {
				return
			}

			slog.Error("blocklist list load failed, keeping the previous one", "url", s.URL.URL, "error", err)

			continue
		}

		s.set = set
	}

	c := compiled{}
	c.allow.merge(&l.static.allow)
	c.deny.merge(&l.static.deny)

	for _, s := range l.sources {
		if s.set == nil {
			continue
		}

		if s.deny {
			c.deny.merge(s.set)
		} else {
			c.allow.merge(s.set)
		}
	}

	l.current.Store(&c)
}

func (l *lists) fetch(ctx context.Context, u URL) (*ruleSet, error) {
	body, err := l.open(ctx, u.URL)
	if err != nil {
		return nil, err
	}
	defer body.Close()

	set := &ruleSet{}
	invalid := 0

	scanner := bufio.NewScanner(io.LimitReader(body, maxListSize))
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// ip and rdns lists often carry trailing comments, e.g. "1.2.3.0/24 ; SBL1"
		if u.Type == typeIP || u.Type == typeRDNS {
			line = strings.Fields(line)[0]
			line, _, _ = strings.Cut(line, ";")
			line, _, _ = strings.Cut(line, "#")
			if line == "" {
				continue
			}
		}

		if err := set.add(u.Type, line); err != nil {
			invalid++
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	if invalid > 0 {
		slog.Warn("blocklist list has invalid entries, skipped", "url", u.URL, "count", invalid)
	}

	return set, nil
}

func (l *lists) open(ctx context.Context, raw string) (io.ReadCloser, error) {
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
		if err != nil {
			return nil, err
		}

		resp, err := l.client.Do(req)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()

			return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
		}

		return resp.Body, nil
	}

	path := raw
	if strings.HasPrefix(raw, "file://") {
		u, err := url.Parse(raw)
		if err != nil {
			return nil, err
		}

		path = u.Path
		if u.Host != "" && u.Host != "localhost" {
			// file://relative/path
			path = u.Host + u.Path
		}
	} else if strings.Contains(raw, "://") {
		return nil, errors.New("unsupported scheme, expected http, https or file")
	}

	return os.Open(path)
}

// check reports whether the request may pass.
func (l *lists) check(ctx context.Context, r *http.Request, ip netip.Addr, dns *lookups) bool {
	c := l.current.Load()

	if !c.allow.empty() && c.allow.match(ctx, r, ip, dns, true) {
		return true
	}

	if c.deny.match(ctx, r, ip, dns, false) {
		return false
	}

	if dns != nil && ip.IsValid() && dns.listed(ctx, ip) {
		return false
	}

	return true
}

func parsePrefix(s string) (netip.Prefix, error) {
	if strings.Contains(s, "/") {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return netip.Prefix{}, err
		}

		if p.Addr().Is4In6() {
			p = netip.PrefixFrom(p.Addr().Unmap(), max(p.Bits()-96, 0))
		}

		return p.Masked(), nil
	}

	a, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Prefix{}, err
	}

	a = a.Unmap()

	return netip.PrefixFrom(a, a.BitLen()), nil
}

// cleanPath is path.Clean that keeps a trailing slash.
func cleanPath(p string) string {
	if p == "" {
		return "/"
	}

	c := path.Clean("/" + p)
	if c != "/" && strings.HasSuffix(p, "/") {
		c += "/"
	}

	return c
}

func normalizeDomain(s string) string {
	return strings.Trim(strings.ToLower(strings.TrimSpace(s)), ".")
}
