package blocklist

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// resolver is net.Resolver, replaceable in tests.
type resolver interface {
	LookupHost(ctx context.Context, host string) ([]string, error)
	LookupAddr(ctx context.Context, addr string) ([]string, error)
}

type netResolver struct {
	net.Resolver
}

// failedTTL caps caching of temporary failures such as timeouts.
const failedTTL = time.Minute

type lookupsConfig struct {
	ttl     time.Duration
	timeout time.Duration
	size    int
	verify  bool
	zones   []string
}

// lookups resolves and caches reverse DNS names and DNSBL listings.
type lookups struct {
	lookupsConfig

	ctx      context.Context
	resolver resolver
	group    singleflight.Group

	mu    sync.Mutex
	cache map[string]cacheEntry
}

type cacheEntry struct {
	names   []string
	listed  bool
	expires time.Time
}

func newLookups(ctx context.Context, res resolver, cfg lookupsConfig) *lookups {
	l := &lookups{
		lookupsConfig: cfg,
		ctx:           ctx,
		resolver:      res,
		cache:         make(map[string]cacheEntry),
	}

	go l.cleanup(ctx, min(cfg.ttl, 10*time.Minute))

	return l
}

func (l *lookups) get(key string) (cacheEntry, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	e, ok := l.cache[key]
	if !ok {
		return e, false
	}

	if !time.Now().Before(e.expires) {
		delete(l.cache, key)

		return e, false
	}

	return e, true
}

func (l *lookups) set(key string, e cacheEntry, ttl time.Duration) {
	e.expires = time.Now().Add(ttl)

	l.mu.Lock()
	defer l.mu.Unlock()

	if _, ok := l.cache[key]; !ok && len(l.cache) >= l.size {
		l.evict()
	}

	l.cache[key] = e
}

// evict drops expired entries, or a tenth of the cache when none expired.
func (l *lookups) evict() {
	now := time.Now()
	for k, e := range l.cache {
		if !now.Before(e.expires) {
			delete(l.cache, k)
		}
	}

	drop := len(l.cache) - l.size + max(l.size/10, 1)
	for k := range l.cache {
		if drop <= 0 {
			break
		}

		delete(l.cache, k)
		drop--
	}
}

func (l *lookups) cleanup(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			l.mu.Lock()
			for k, e := range l.cache {
				if !now.Before(e.expires) {
					delete(l.cache, k)
				}
			}
			l.mu.Unlock()
		}
	}
}

// resolve runs fn once per key for concurrent callers and caches the result.
// The lookup is not bound to the request, a cancelled request only stops
// waiting for it.
func (l *lookups) resolve(ctx context.Context, key string, fn func(context.Context) (cacheEntry, bool)) cacheEntry {
	if e, ok := l.get(key); ok {
		return e
	}

	ch := l.group.DoChan(key, func() (any, error) {
		lctx, cancel := context.WithTimeout(l.ctx, l.timeout)
		defer cancel()

		e, temporary := fn(lctx)

		ttl := l.ttl
		if temporary {
			ttl = min(ttl, failedTTL)
		}
		l.set(key, e, ttl)

		return e, nil
	})

	select {
	case <-ctx.Done():
		return cacheEntry{}
	case res := <-ch:
		e, _ := res.Val.(cacheEntry)

		return e
	}
}

// names returns the lowercase reverse DNS names of ip, forward confirmed when
// verify is set.
func (l *lookups) names(ctx context.Context, ip netip.Addr) []string {
	return l.resolve(ctx, "ptr:"+ip.String(), func(ctx context.Context) (cacheEntry, bool) {
		ptrs, err := l.resolver.LookupAddr(ctx, ip.String())
		if err != nil && len(ptrs) == 0 {
			return cacheEntry{}, temporary(err)
		}

		var names []string
		failed := false
		for _, n := range ptrs {
			n = normalizeDomain(n)
			if n == "" {
				continue
			}

			if l.verify {
				ok, tmp := l.forwardConfirmed(ctx, n, ip)
				failed = failed || tmp
				if !ok {
					continue
				}
			}

			names = append(names, n)
		}

		// a slow forward zone must not hide the name for the whole ttl
		return cacheEntry{names: names}, failed
	}).names
}

// forwardConfirmed reports whether name resolves to ip, and whether the
// lookup failed temporarily.
func (l *lookups) forwardConfirmed(ctx context.Context, name string, ip netip.Addr) (bool, bool) {
	addrs, err := l.resolver.LookupHost(ctx, name)
	if err != nil && len(addrs) == 0 {
		return false, temporary(err)
	}

	return slices.ContainsFunc(addrs, func(a string) bool {
		got, err := netip.ParseAddr(a)

		return err == nil && got.Unmap() == ip
	}), false
}

// listed reports whether ip is in any DNSBL zone.
func (l *lookups) listed(ctx context.Context, ip netip.Addr) bool {
	if len(l.zones) == 0 {
		return false
	}

	return l.resolve(ctx, "dnsbl:"+ip.String(), func(ctx context.Context) (cacheEntry, bool) {
		query := reverseName(ip)
		failed := false

		for _, zone := range l.zones {
			addrs, err := l.resolver.LookupHost(ctx, query+"."+zone)
			if slices.ContainsFunc(addrs, listingCode) {
				return cacheEntry{listed: true}, false
			}
			if len(addrs) > 0 {
				slog.Warn("blocklist dnsbl answer is not a listing, check the resolver", "zone", zone, "answer", addrs)
				failed = true
			}

			if err != nil && temporary(err) {
				failed = true
			}
		}

		return cacheEntry{}, failed
	}).listed
}

// listingCode reports whether a DNSBL answer is a listing: 127.0.0.0/8
// except 127.255.255.0/24, which Spamhaus uses for errors such as queries
// from public resolvers. Other answers come from resolvers rewriting
// NXDOMAIN and would block every client.
func listingCode(a string) bool {
	ip, err := netip.ParseAddr(a)
	if err != nil {
		return false
	}

	ip = ip.Unmap()
	if !ip.Is4() {
		return false
	}

	b := ip.As4()

	return b[0] == 127 && (b[1] != 255 || b[2] != 255)
}

// temporary reports whether a lookup error may succeed later; not found
// answers are final.
func temporary(err error) bool {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return !dnsErr.IsNotFound
	}

	return true
}

// reverseName returns the DNSBL query label of ip: reversed octets for IPv4,
// reversed nibbles for IPv6.
func reverseName(ip netip.Addr) string {
	var b strings.Builder

	if ip.Is4() {
		a := ip.As4()
		for i := 3; i >= 0; i-- {
			b.WriteString(strconv.Itoa(int(a[i])))
			if i > 0 {
				b.WriteByte('.')
			}
		}

		return b.String()
	}

	const hex = "0123456789abcdef"

	a := ip.As16()
	for i := 15; i >= 0; i-- {
		b.WriteByte(hex[a[i]&0x0f])
		b.WriteByte('.')
		b.WriteByte(hex[a[i]>>4])
		if i > 0 {
			b.WriteByte('.')
		}
	}

	return b.String()
}

func normalizeZones(zones []string) ([]string, error) {
	out := make([]string, 0, len(zones))
	for _, z := range zones {
		n := normalizeDomain(z)
		if n == "" {
			return nil, fmt.Errorf("blocklist dnsbl zone %q is empty", z)
		}

		out = append(out, n)
	}

	return out, nil
}
