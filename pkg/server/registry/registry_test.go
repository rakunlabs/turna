package registry

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestNamedTransaction(t *testing.T) {
	n := NewNamed[string]("test-tx")
	n.Set("a", "1")

	Begin()
	n.Set("a", "2")
	n.Set("b", "new")
	n.Delete("a")
	n.Set("a", "3")

	if v, _ := n.Get("a"); v != "1" {
		t.Fatalf("Get during tx = %q, want committed value", v)
	}
	if v, _ := n.Resolve("a"); v != "3" {
		t.Fatalf("Resolve during tx = %q", v)
	}
	if _, ok := n.Get("b"); ok {
		t.Fatal("staged value visible to Get")
	}

	Rollback()

	if v, _ := n.Get("a"); v != "1" {
		t.Fatalf("after rollback = %q", v)
	}
	if _, ok := n.Resolve("b"); ok {
		t.Fatal("rolled back value still resolvable")
	}

	Begin()
	n.Delete("a")
	n.Set("b", "x")
	Commit()

	if _, ok := n.Get("a"); ok {
		t.Fatal("deleted value still present")
	}
	if v, _ := n.Get("b"); v != "x" {
		t.Fatalf("committed value = %q", v)
	}
}

func TestComponentsRebuildDependents(t *testing.T) {
	prev := DrainTimeout
	DrainTimeout = 0
	t.Cleanup(func() { DrainTimeout = prev })

	c := NewComponents("test-components")
	builds := map[string]int{}
	stopped := map[string]int{}
	var mu sync.Mutex

	spec := func(name, fp string, refs ...string) Spec {
		return Spec{Fingerprint: fp, Refs: refs, Build: func(ctx context.Context) error {
			builds[name]++
			context.AfterFunc(ctx, func() {
				mu.Lock()
				stopped[name]++
				mu.Unlock()
			})

			return nil
		}}
	}

	stoppedCount := func(name string, want int) {
		t.Helper()

		deadline := time.Now().Add(time.Second)
		for {
			mu.Lock()
			got := stopped[name]
			mu.Unlock()

			if got == want {
				return
			}

			if time.Now().After(deadline) {
				t.Fatalf("%s stopped %d times, want %d", name, got, want)
			}

			time.Sleep(5 * time.Millisecond)
		}
	}

	apply := func(specs map[string]Spec) error {
		Begin()

		s, err := c.Stage(context.Background(), specs)
		if err != nil {
			Rollback()

			return err
		}

		Commit()
		s.Commit()

		return nil
	}

	if err := apply(map[string]Spec{"session": spec("session", "1"), "login": spec("login", "1", "session"), "other": spec("other", "1")}); err != nil {
		t.Fatal(err)
	}

	// changing session rebuilds login, other is kept
	if err := apply(map[string]Spec{"session": spec("session", "2"), "login": spec("login", "1", "session"), "other": spec("other", "1")}); err != nil {
		t.Fatal(err)
	}

	if builds["session"] != 2 || builds["login"] != 2 || builds["other"] != 1 {
		t.Fatalf("builds = %v", builds)
	}

	// failing build stops staged components and keeps the running ones
	failing := Spec{Fingerprint: "x", Build: func(context.Context) error { return errors.New("boom") }}
	if err := apply(map[string]Spec{"session": spec("session", "3"), "bad": failing}); err == nil {
		t.Fatal("expected error")
	}

	stoppedCount("login", 1)
	stoppedCount("other", 0)

	// removed components are stopped
	if err := apply(map[string]Spec{"session": spec("session", "2")}); err != nil {
		t.Fatal(err)
	}

	stoppedCount("login", 2)
	stoppedCount("other", 1)
}
