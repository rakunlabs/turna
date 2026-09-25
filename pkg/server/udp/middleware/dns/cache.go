package dns

import (
	"container/list"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
)

// Cache configures caching of upstream answers.
type Cache struct {
	// Size is the maximum number of cached answers, default is 10000.
	Size int `cfg:"size"`
	// MinTTL raises lower record TTLs, default is 0.
	MinTTL time.Duration `cfg:"min_ttl"`
	// MaxTTL caps record TTLs, default is 1h.
	MaxTTL time.Duration `cfg:"max_ttl"`
	// NegativeTTL is used for NXDOMAIN/NODATA without SOA, default is 30s.
	NegativeTTL time.Duration `cfg:"negative_ttl"`
}

type cacheEntry struct {
	key     string
	msg     *dns.Msg
	stored  time.Time
	expires time.Time
}

type cache struct {
	cfg Cache

	mu    sync.Mutex
	items map[string]*list.Element
	lru   *list.List
	now   func() time.Time
}

func newCache(cfg Cache) *cache {
	if cfg.Size <= 0 {
		cfg.Size = 10000
	}

	if cfg.MaxTTL <= 0 {
		cfg.MaxTTL = time.Hour
	}

	if cfg.NegativeTTL <= 0 {
		cfg.NegativeTTL = 30 * time.Second
	}

	return &cache{
		cfg:   cfg,
		items: make(map[string]*list.Element),
		lru:   list.New(),
		now:   time.Now,
	}
}

func cacheKey(req *dns.Msg) (string, bool) {
	if len(req.Question) != 1 {
		return "", false
	}

	q := req.Question[0]

	var sb strings.Builder
	sb.WriteString(strings.ToLower(q.Name))
	sb.WriteByte('|')
	sb.WriteString(dns.TypeToString[q.Qtype])
	sb.WriteByte('|')
	sb.WriteString(dns.ClassToString[q.Qclass])

	if req.CheckingDisabled {
		sb.WriteString("|cd")
	}

	if opt := req.IsEdns0(); opt != nil && opt.Do() {
		sb.WriteString("|do")
	}

	return sb.String(), true
}

// get returns a copy of a cached answer with TTLs decreased by its age.
func (c *cache) get(req *dns.Msg) *dns.Msg {
	key, ok := cacheKey(req)
	if !ok {
		return nil
	}

	now := c.now()

	c.mu.Lock()
	defer c.mu.Unlock()

	el, ok := c.items[key]
	if !ok {
		return nil
	}

	e := el.Value.(*cacheEntry)
	if !now.Before(e.expires) {
		c.lru.Remove(el)
		delete(c.items, key)

		return nil
	}

	c.lru.MoveToFront(el)

	age := uint32(now.Sub(e.stored) / time.Second)

	resp := e.msg.Copy()
	resp.Id = req.Id
	resp.Question = req.Question

	for _, section := range [][]dns.RR{resp.Answer, resp.Ns, resp.Extra} {
		for _, rr := range section {
			if rr.Header().Rrtype == dns.TypeOPT {
				continue
			}

			if rr.Header().Ttl > age {
				rr.Header().Ttl -= age
			} else {
				rr.Header().Ttl = 0
			}
		}
	}

	return resp
}

func (c *cache) set(req, resp *dns.Msg) {
	if resp.Truncated {
		return
	}

	switch resp.Rcode {
	case dns.RcodeSuccess, dns.RcodeNameError:
	default:
		return
	}

	key, ok := cacheKey(req)
	if !ok {
		return
	}

	ttl, ok := c.ttl(resp)
	if !ok || ttl <= 0 {
		return
	}

	now := c.now()
	e := &cacheEntry{key: key, msg: resp.Copy(), stored: now, expires: now.Add(ttl)}

	c.mu.Lock()
	defer c.mu.Unlock()

	if el, ok := c.items[key]; ok {
		el.Value = e
		c.lru.MoveToFront(el)

		return
	}

	c.items[key] = c.lru.PushFront(e)

	for c.lru.Len() > c.cfg.Size {
		last := c.lru.Back()
		c.lru.Remove(last)
		delete(c.items, last.Value.(*cacheEntry).key)
	}
}

// ttl is the lowest record TTL, clamped. Negative answers use the SOA
// minimum (RFC 2308) or NegativeTTL.
func (c *cache) ttl(resp *dns.Msg) (time.Duration, bool) {
	var (
		lowest uint32
		found  bool
	)

	if len(resp.Answer) == 0 {
		for _, rr := range resp.Ns {
			if soa, ok := rr.(*dns.SOA); ok {
				lowest = soa.Minttl
				if soa.Hdr.Ttl < lowest {
					lowest = soa.Hdr.Ttl
				}

				found = true
			}
		}

		if !found {
			return c.cfg.NegativeTTL, true
		}
	} else {
		for _, rr := range resp.Answer {
			if !found || rr.Header().Ttl < lowest {
				lowest = rr.Header().Ttl
				found = true
			}
		}
	}

	ttl := time.Duration(lowest) * time.Second
	ttl = max(ttl, c.cfg.MinTTL)
	ttl = min(ttl, c.cfg.MaxTTL)

	return ttl, true
}
