package service

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	httputil2 "github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/http/tcontext"
)

const (
	StrategyRoundRobin = "round_robin"
	StrategyRandom     = "random"
	StrategyLeastConn  = "least_conn"
	StrategyHash       = "hash"
	StrategyFirst      = "first"

	triedTargetsKey = "_lb_tried_targets"
)

// targetState holds the runtime state of an upstream used by the balancer,
// health checks and the proxy.
type targetState struct {
	id string

	// healthy is the result of active health checks.
	healthy atomic.Bool
	// active is the number of in-flight requests.
	active atomic.Int64

	passive *PassiveHealthCheck

	mu           sync.Mutex
	fails        int
	failStart    time.Time
	ejectedUntil time.Time
}

func newTargetState(u *url.URL, passive *PassiveHealthCheck) *targetState {
	sum := sha256.Sum256([]byte(u.String()))

	s := &targetState{
		id:      hex.EncodeToString(sum[:8]),
		passive: passive,
	}
	s.healthy.Store(true)

	return s
}

func (s *targetState) available(now time.Time) bool {
	if !s.healthy.Load() {
		return false
	}

	if s.passive == nil {
		return true
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	return !now.Before(s.ejectedUntil)
}

// report records the result of a proxied request for passive health checks.
func (s *targetState) report(ok bool, name string) {
	if s.passive == nil || s.passive.MaxFails <= 0 {
		return
	}

	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	if ok {
		return
	}

	window := s.passive.failTimeout()
	if s.fails == 0 || now.Sub(s.failStart) > window {
		s.fails = 0
		s.failStart = now
	}

	s.fails++
	if s.fails >= s.passive.MaxFails {
		s.fails = 0
		s.ejectedUntil = now.Add(window)
		slog.Warn("upstream ejected by passive health check", "target", name, "duration", window.String())
	}
}

func (t *ProxyTarget) getWeight() int {
	if t.Weight <= 0 {
		return 1
	}

	return t.Weight
}

func (t *ProxyTarget) acquire() {
	if t.state != nil {
		t.state.active.Add(1)
	}
}

func (t *ProxyTarget) release() {
	if t.state != nil {
		t.state.active.Add(-1)
	}
}

func (t *ProxyTarget) report(ok bool) {
	if t.state != nil {
		t.state.report(ok, t.URL.String())
	}
}

// ///////////////////////////////////////////////////////////////////////////

// lbBalancer is a health aware balancer supporting weights, several
// strategies and cookie based session affinity.
type lbBalancer struct {
	targets  []*ProxyTarget
	strategy string
	sticky   *stickyCookie
	// key builds the hash key of a request for the hash strategy.
	key func(r *http.Request) string

	mu sync.Mutex
	rr uint64
}

func newLBBalancer(targets []*ProxyTarget, strategy string, sticky *stickyCookie, key func(r *http.Request) string) (*lbBalancer, error) {
	switch strategy {
	case "":
		strategy = StrategyRoundRobin
	case StrategyRoundRobin, StrategyRandom, StrategyLeastConn, StrategyHash, StrategyFirst:
	default:
		return nil, fmt.Errorf("unsupported strategy %q (supported: %s, %s, %s, %s, %s)",
			strategy, StrategyRoundRobin, StrategyRandom, StrategyLeastConn, StrategyHash, StrategyFirst)
	}

	if strategy == StrategyHash && key == nil {
		return nil, fmt.Errorf("hash strategy requires a key")
	}

	return &lbBalancer{
		targets:  targets,
		strategy: strategy,
		sticky:   sticky,
		key:      key,
	}, nil
}

func (b *lbBalancer) AddTarget(target *ProxyTarget) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	for _, t := range b.targets {
		if t.Name == target.Name {
			return false
		}
	}

	if target.state == nil {
		target.state = newTargetState(target.URL, nil)
	}

	b.targets = append(b.targets, target)

	return true
}

func (b *lbBalancer) RemoveTarget(name string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	for i, t := range b.targets {
		if t.Name == name {
			b.targets = slices.Delete(b.targets, i, i+1)

			return true
		}
	}

	return false
}

func (b *lbBalancer) Next(w http.ResponseWriter, r *http.Request) *ProxyTarget {
	t, _ := b.NextTarget(w, r)

	return t
}

var errNoUpstream = httputil2.NewError("no available upstream", nil, http.StatusServiceUnavailable)

func (b *lbBalancer) NextTarget(w http.ResponseWriter, r *http.Request) (*ProxyTarget, error) {
	// build the hash key before locking, it may parse the query or cookies
	var key string
	if b.strategy == StrategyHash {
		key = b.key(r)
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if len(b.targets) == 0 {
		return nil, errNoUpstream
	}

	tried, _ := tcontext.Get(r, triedTargetsKey).([]string)
	now := time.Now()

	candidates := make([]*ProxyTarget, 0, len(b.targets))
	for _, t := range b.targets {
		if !slices.Contains(tried, t.state.id) && t.state.available(now) {
			candidates = append(candidates, t)
		}
	}

	if len(candidates) == 0 {
		if len(tried) > 0 {
			// every healthy target already failed for this request
			return nil, errNoUpstream
		}

		// fail open: when everything looks down, still try to serve
		candidates = b.targets
	}

	var target *ProxyTarget

	// session affinity is only used for the first attempt
	if b.sticky != nil && len(tried) == 0 {
		if id := b.sticky.get(r); id != "" {
			for _, t := range candidates {
				if t.state.id == id {
					target = t

					break
				}
			}
		}
	}

	stickyHit := target != nil

	if target == nil {
		target = b.pick(key, candidates)
	}

	if b.sticky != nil && !stickyHit {
		b.sticky.set(w, target.state.id)
	}

	tcontext.Set(r, triedTargetsKey, append(tried, target.state.id))

	return target, nil
}

func (b *lbBalancer) pick(key string, candidates []*ProxyTarget) *ProxyTarget {
	if len(candidates) == 1 {
		return candidates[0]
	}

	switch b.strategy {
	case StrategyFirst:
		return candidates[0]
	case StrategyHash:
		return rendezvous(key, candidates)
	case StrategyRandom:
		total := 0
		for _, t := range candidates {
			total += t.getWeight()
		}

		n := rand.IntN(total)
		for _, t := range candidates {
			n -= t.getWeight()
			if n < 0 {
				return t
			}
		}

		return candidates[len(candidates)-1]
	case StrategyLeastConn:
		// compare active/weight without division; start from a rotating
		// offset so ties are spread across targets
		b.rr++
		offset := int(b.rr % uint64(len(candidates)))

		var best *ProxyTarget
		for i := range candidates {
			t := candidates[(i+offset)%len(candidates)]
			if best == nil || t.state.active.Load()*int64(best.getWeight()) < best.state.active.Load()*int64(t.getWeight()) {
				best = t
			}
		}

		return best
	default:
		// smooth weighted round-robin (nginx)
		total := 0

		var best *ProxyTarget
		for _, t := range candidates {
			t.current += t.getWeight()
			total += t.getWeight()

			if best == nil || t.current > best.current {
				best = t
			}
		}

		best.current -= total

		return best
	}
}

// rendezvous picks the target with the highest weighted score for the key
// (weighted HRW), so removing a target only moves the keys it owned.
func rendezvous(key string, candidates []*ProxyTarget) *ProxyTarget {
	var (
		best      *ProxyTarget
		bestScore float64
	)

	for _, t := range candidates {
		h := fnvString(fnvString(fnvOffset64, key)*fnvPrime64, t.state.id)
		score := float64(t.getWeight()) / -math.Log(unitOpen(mix64(h)))

		if best == nil || score > bestScore {
			best = t
			bestScore = score
		}
	}

	return best
}

// unitOpen maps x to (0, 1) using 52 bits, the +0.5 stays exact so the
// result is never 0 or 1 and its logarithm is finite and negative.
func unitOpen(x uint64) float64 {
	return (float64(x>>12) + 0.5) / (1 << 52)
}

const (
	fnvOffset64 = 14695981039346656037
	fnvPrime64  = 1099511628211
)

// fnvString continues an FNV-1a hash with s without allocating; multiplying
// by the prime in between separates key and target id like a zero byte.
func fnvString(h uint64, s string) uint64 {
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= fnvPrime64
	}

	return h
}

// mix64 is the splitmix64 finalizer, spreading FNV output over all bits.
func mix64(x uint64) uint64 {
	x ^= x >> 30
	x *= 0xbf58476d1ce4e5b9
	x ^= x >> 27
	x *= 0x94d049bb133111eb
	x ^= x >> 31

	return x
}

// ///////////////////////////////////////////////////////////////////////////

type Sticky struct {
	Cookie *StickyCookie `cfg:"cookie"`
}

type StickyCookie struct {
	// Name of the cookie, default is "turna_lb".
	Name string `cfg:"name"`
	// Path default is "/".
	Path     string        `cfg:"path"`
	Domain   string        `cfg:"domain"`
	MaxAge   time.Duration `cfg:"max_age"`
	Secure   bool          `cfg:"secure"`
	HTTPOnly *bool         `cfg:"http_only"`
	// SameSite is lax, strict or none; default is lax.
	SameSite string `cfg:"same_site"`
}

type stickyCookie struct {
	cookie   http.Cookie
	sameSite http.SameSite
}

func (s *Sticky) build(suffix string) (*stickyCookie, error) {
	if s == nil || s.Cookie == nil {
		return nil, nil
	}

	c := s.Cookie

	name := c.Name
	if name == "" {
		name = "turna_lb"
	}

	path := c.Path
	if path == "" {
		path = "/"
	}

	httpOnly := true
	if c.HTTPOnly != nil {
		httpOnly = *c.HTTPOnly
	}

	var sameSite http.SameSite
	switch strings.ToLower(c.SameSite) {
	case "", "lax":
		sameSite = http.SameSiteLaxMode
	case "strict":
		sameSite = http.SameSiteStrictMode
	case "none":
		sameSite = http.SameSiteNoneMode
	default:
		return nil, fmt.Errorf("unsupported sticky cookie same_site %q", c.SameSite)
	}

	return &stickyCookie{
		cookie: http.Cookie{
			Name:     name + suffix,
			Path:     path,
			Domain:   c.Domain,
			MaxAge:   int(c.MaxAge.Seconds()),
			Secure:   c.Secure,
			HttpOnly: httpOnly,
			SameSite: sameSite,
		},
	}, nil
}

func (s *stickyCookie) get(r *http.Request) string {
	c, err := r.Cookie(s.cookie.Name)
	if err != nil {
		return ""
	}

	return c.Value
}

func (s *stickyCookie) set(w http.ResponseWriter, id string) {
	c := s.cookie
	c.Value = id

	http.SetCookie(w, &c)
}
