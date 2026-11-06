package service

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/internal/requestkey"
	"github.com/rakunlabs/turna/pkg/server/http/tcontext"
)

func testTargets(t *testing.T, weights ...int) []*ProxyTarget {
	t.Helper()

	targets := make([]*ProxyTarget, 0, len(weights))
	for i, w := range weights {
		u := mustURL(t, fmt.Sprintf("http://backend-%d", i))
		targets = append(targets, &ProxyTarget{Name: u.String(), URL: u, Weight: w, state: newTargetState(u, nil)})
	}

	return targets
}

func newHashBalancer(t *testing.T, targets []*ProxyTarget) *lbBalancer {
	t.Helper()

	key, err := requestkey.New("test", []string{"header:X-Key"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	lb, err := newLBBalancer(targets, StrategyHash, nil, key)
	if err != nil {
		t.Fatal(err)
	}

	return lb
}

func keyRequest(key string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Key", key)
	_, req = tcontext.New(nil, req)

	return req
}

func pickName(t *testing.T, lb *lbBalancer, req *http.Request) string {
	t.Helper()

	tgt, err := lb.NextTarget(httptest.NewRecorder(), req)
	if err != nil {
		t.Fatal(err)
	}

	return tgt.Name
}

func TestHashStable(t *testing.T) {
	lb := newHashBalancer(t, testTargets(t, 1, 1, 1, 1))

	used := map[string]bool{}
	for i := range 200 {
		key := fmt.Sprintf("user-%d", i)
		first := pickName(t, lb, keyRequest(key))
		used[first] = true

		for range 3 {
			if got := pickName(t, lb, keyRequest(key)); got != first {
				t.Fatalf("key %s moved from %s to %s", key, first, got)
			}
		}
	}

	if len(used) != 4 {
		t.Fatalf("expected keys on all targets, got %v", used)
	}
}

func TestHashOnlyFailedTargetKeysMove(t *testing.T) {
	targets := testTargets(t, 1, 1, 1, 1)
	lb := newHashBalancer(t, targets)

	before := map[string]string{}
	for i := range 1000 {
		key := fmt.Sprintf("user-%d", i)
		before[key] = pickName(t, lb, keyRequest(key))
	}

	down := targets[1]
	down.state.healthy.Store(false)

	moved := 0
	for key, prev := range before {
		got := pickName(t, lb, keyRequest(key))
		if got == down.Name {
			t.Fatalf("key %s still on unhealthy target", key)
		}

		if prev != down.Name && got != prev {
			t.Fatalf("key %s moved from healthy %s to %s", key, prev, got)
		}

		if prev == down.Name {
			moved++
		}
	}

	if moved == 0 {
		t.Fatal("no key was on the unhealthy target")
	}

	down.state.healthy.Store(true)

	for key, prev := range before {
		if got := pickName(t, lb, keyRequest(key)); got != prev {
			t.Fatalf("key %s did not return to %s after recovery, got %s", key, prev, got)
		}
	}
}

func TestHashWeights(t *testing.T) {
	targets := testTargets(t, 3, 1)
	lb := newHashBalancer(t, targets)

	counts := map[string]int{}
	for i := range 4000 {
		counts[pickName(t, lb, keyRequest(fmt.Sprintf("user-%d", i)))]++
	}

	share := float64(counts[targets[0].Name]) / 4000
	if share < 0.70 || share > 0.80 {
		t.Fatalf("weight 3 of 4 got share %.3f (%v)", share, counts)
	}
}

func TestHashRetryPicksNextTarget(t *testing.T) {
	lb := newHashBalancer(t, testTargets(t, 1, 1, 1))

	for i := range 50 {
		key := fmt.Sprintf("user-%d", i)

		req := keyRequest(key)
		first := pickName(t, lb, req)
		second := pickName(t, lb, req)

		if first == second {
			t.Fatalf("retry of %s picked the same target %s", key, first)
		}

		// the retry order is deterministic per key
		req = keyRequest(key)
		if pickName(t, lb, req) != first || pickName(t, lb, req) != second {
			t.Fatalf("retry order of %s is not stable", key)
		}
	}
}

func TestHashStickyPrecedence(t *testing.T) {
	targets := testTargets(t, 1, 1)
	lb := newHashBalancer(t, targets)
	lb.sticky, _ = (&Sticky{Cookie: &StickyCookie{}}).build("")

	req := keyRequest("user-1")
	hashed := pickName(t, lb, req)

	other := targets[0]
	if other.Name == hashed {
		other = targets[1]
	}

	req = keyRequest("user-1")
	req.AddCookie(&http.Cookie{Name: "turna_lb", Value: other.state.id})

	if got := pickName(t, lb, req); got != other.Name {
		t.Fatalf("sticky cookie ignored: got %s want %s", got, other.Name)
	}
}

func TestFirstStrategyFailover(t *testing.T) {
	targets := testTargets(t, 1, 5, 1)
	lb, err := newLBBalancer(targets, StrategyFirst, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	next := func() string { return pickName(t, lb, keyRequest("")) }

	for range 5 {
		if got := next(); got != targets[0].Name {
			t.Fatalf("got %s want primary", got)
		}
	}

	targets[0].state.healthy.Store(false)
	if got := next(); got != targets[1].Name {
		t.Fatalf("got %s want first backup", got)
	}

	targets[1].state.healthy.Store(false)
	if got := next(); got != targets[2].Name {
		t.Fatalf("got %s want second backup", got)
	}

	targets[0].state.healthy.Store(true)
	if got := next(); got != targets[0].Name {
		t.Fatalf("got %s want recovered primary", got)
	}

	// retries walk the config order
	req := keyRequest("")
	if a, b := pickName(t, lb, req), pickName(t, lb, req); a != targets[0].Name || b != targets[2].Name {
		t.Fatalf("retry order %s, %s", a, b)
	}
}

func TestHashService(t *testing.T) {
	a := namedBackend(t, "a", nil)
	b := namedBackend(t, "b", nil)

	h := newServiceHandler(t, context.Background(), &Service{LoadBalancer: LoadBalancer{
		Strategy: StrategyHash,
		HashKey:  []string{"query:user"},
		Servers:  []Server{{URL: a.URL}, {URL: b.URL}},
	}})

	used := map[string]bool{}
	for i := range 20 {
		var first string
		for j := range 3 {
			req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/?user=%d", i), nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if j == 0 {
				first = rec.Body.String()
			} else if rec.Body.String() != first {
				t.Fatalf("user %d moved from %s to %s", i, first, rec.Body.String())
			}
		}

		used[first] = true
	}

	if len(used) != 2 {
		t.Fatalf("expected both backends used, got %v", used)
	}
}

func TestHashInvalidKey(t *testing.T) {
	m := &Service{LoadBalancer: LoadBalancer{
		Strategy: StrategyHash,
		HashKey:  []string{"bogus"},
		Servers:  []Server{{URL: "http://a"}},
	}}

	if _, err := m.Middleware(context.Background()); err == nil {
		t.Fatal("expected error for invalid hash_key")
	}
}

func TestUnitOpenBounds(t *testing.T) {
	for _, x := range []uint64{0, 1, 1 << 12, math.MaxUint64, math.MaxUint64 - 1} {
		u := unitOpen(x)
		if !(u > 0 && u < 1) {
			t.Fatalf("unitOpen(%#x) = %v, want in (0, 1)", x, u)
		}

		if l := -math.Log(u); math.IsInf(l, 0) || l <= 0 {
			t.Fatalf("unitOpen(%#x): -log = %v", x, l)
		}
	}
}

func TestRendezvousNoAlloc(t *testing.T) {
	targets := testTargets(t, 1, 2, 3)

	if n := testing.AllocsPerRun(100, func() { rendezvous("user-1", targets) }); n != 0 {
		t.Fatalf("rendezvous allocates %v times", n)
	}
}

func TestHashConfigNotMutated(t *testing.T) {
	m := &Service{LoadBalancer: LoadBalancer{
		Strategy:       StrategyHash,
		HashKey:        []string{" Header:X-Key "},
		TrustedProxies: []string{"10.0.0.0/8"},
		Servers:        []Server{{URL: "http://a"}},
	}}

	if _, err := m.Middleware(context.Background()); err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(m.LoadBalancer.HashKey, []string{" Header:X-Key "}) || !slices.Equal(m.LoadBalancer.TrustedProxies, []string{"10.0.0.0/8"}) {
		t.Fatalf("config mutated: %v %v", m.LoadBalancer.HashKey, m.LoadBalancer.TrustedProxies)
	}

	m = &Service{LoadBalancer: LoadBalancer{Strategy: StrategyHash, Servers: []Server{{URL: "http://a"}}}}
	if _, err := m.Middleware(context.Background()); err != nil {
		t.Fatal(err)
	}

	if m.LoadBalancer.HashKey != nil {
		t.Fatalf("default hash_key written to config: %v", m.LoadBalancer.HashKey)
	}
}
