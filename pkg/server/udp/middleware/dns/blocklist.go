package dns

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/miekg/dns"
)

const (
	BlockResponseNXDomain = "nxdomain"
	BlockResponseRefused  = "refused"
	BlockResponseNullIP   = "null_ip"
)

// Blocklist blocks queries for domains and their subdomains.
type Blocklist struct {
	// Domains to block, "example.com" also blocks its subdomains.
	Domains []string `cfg:"domains"`
	// Files with one domain per line or hosts file lines
	// ("0.0.0.0 example.com"). "#" starts a comment.
	Files []string `cfg:"files"`
	// Allow overrides the blocklist for these domains and their subdomains.
	Allow []string `cfg:"allow"`
	// Response is nxdomain (default), refused or null_ip (0.0.0.0 / ::).
	Response string `cfg:"response"`
}

type blocklist struct {
	blocked  map[string]struct{}
	allowed  map[string]struct{}
	response string
}

func newBlocklist(cfg *Blocklist) (*blocklist, error) {
	b := &blocklist{
		blocked:  map[string]struct{}{},
		allowed:  map[string]struct{}{},
		response: strings.ToLower(strings.TrimSpace(cfg.Response)),
	}

	switch b.response {
	case "":
		b.response = BlockResponseNXDomain
	case BlockResponseNXDomain, BlockResponseRefused, BlockResponseNullIP:
	default:
		return nil, fmt.Errorf("unsupported blocklist response %q", cfg.Response)
	}

	for _, d := range cfg.Domains {
		b.add(b.blocked, d)
	}

	for _, d := range cfg.Allow {
		b.add(b.allowed, d)
	}

	for _, f := range cfg.Files {
		if err := b.loadFile(f); err != nil {
			return nil, err
		}
	}

	return b, nil
}

func (b *blocklist) add(set map[string]struct{}, domain string) {
	domain = strings.TrimPrefix(strings.TrimSpace(domain), "*.")
	if domain == "" {
		return
	}

	set[dns.CanonicalName(domain)] = struct{}{}
}

func (b *blocklist) loadFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open blocklist file: %w", err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line, _, _ := strings.Cut(sc.Text(), "#")

		fields := strings.Fields(line)
		switch len(fields) {
		case 0:
			continue
		case 1:
			b.add(b.blocked, fields[0])
		default:
			// hosts format: address followed by names
			if net.ParseIP(fields[0]) == nil {
				continue
			}

			for _, name := range fields[1:] {
				if name == "localhost" || strings.HasPrefix(name, "localhost.") {
					continue
				}

				b.add(b.blocked, name)
			}
		}
	}

	if err := sc.Err(); err != nil {
		return fmt.Errorf("read blocklist file %s: %w", path, err)
	}

	return nil
}

// matchSet reports whether qname or one of its parents is in set.
func matchSet(set map[string]struct{}, qname string) bool {
	if len(set) == 0 {
		return false
	}

	for name := qname; ; {
		if _, ok := set[name]; ok {
			return true
		}

		i, end := dns.NextLabel(name, 0)
		if end {
			return false
		}

		name = name[i:]
	}
}

func (b *blocklist) blocks(qname string) bool {
	return matchSet(b.blocked, qname) && !matchSet(b.allowed, qname)
}

func (b *blocklist) reply(req *dns.Msg) *dns.Msg {
	m := new(dns.Msg)

	switch b.response {
	case BlockResponseRefused:
		return m.SetRcode(req, dns.RcodeRefused)
	case BlockResponseNullIP:
		m.SetReply(req)

		q := req.Question[0]
		hdr := dns.RR_Header{Name: q.Name, Class: dns.ClassINET, Ttl: 60}

		switch q.Qtype {
		case dns.TypeA:
			hdr.Rrtype = dns.TypeA
			m.Answer = []dns.RR{&dns.A{Hdr: hdr, A: net.IPv4zero}}
		case dns.TypeAAAA:
			hdr.Rrtype = dns.TypeAAAA
			m.Answer = []dns.RR{&dns.AAAA{Hdr: hdr, AAAA: net.IPv6zero}}
		}

		return m
	}

	return m.SetRcode(req, dns.RcodeNameError)
}
