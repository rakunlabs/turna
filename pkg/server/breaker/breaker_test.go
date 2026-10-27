package breaker

import (
	"testing"
	"time"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func TestBreakerStates(t *testing.T) {
	b, err := New("test", Config{MinRequests: 4, FailureRatio: 0.5, OpenDuration: time.Minute, HalfOpenRequests: 2})
	if err != nil {
		t.Fatal(err)
	}

	c := &clock{t: time.Unix(1_000_000, 0)}
	b.now = c.now

	pass := func(failed bool) {
		t.Helper()

		gen, ok, _ := b.Allow()
		if !ok {
			t.Fatal("request rejected")
		}

		b.Report(gen, failed)
	}

	pass(false)
	pass(true)
	pass(false)
	if b.state != closed {
		t.Fatal("opened below min_requests")
	}

	pass(true) // 2 of 4
	if b.state != open {
		t.Fatal("not opened at failure ratio")
	}

	if _, ok, wait := b.Allow(); ok || wait != time.Minute {
		t.Fatalf("open circuit allowed request, wait %v", wait)
	}

	c.t = c.t.Add(time.Minute)

	g1, ok1, _ := b.Allow()
	g2, ok2, _ := b.Allow()
	_, ok3, _ := b.Allow()
	if !ok1 || !ok2 || ok3 {
		t.Fatalf("half open probes = %v %v %v, want 2 allowed", ok1, ok2, ok3)
	}

	b.Report(g1, false)
	if b.state != halfOpen {
		t.Fatal("closed before all probes succeeded")
	}

	b.Report(g2, false)
	if b.state != closed {
		t.Fatal("not closed after probes succeeded")
	}

	// a failed probe opens again
	for range 4 {
		pass(true)
	}
	c.t = c.t.Add(time.Minute)

	g, _, _ := b.Allow()
	b.Report(g, true)
	if b.state != open {
		t.Fatal("failed probe did not open the circuit")
	}
}

func TestBreakerWindow(t *testing.T) {
	b, err := New("test", Config{MinRequests: 2, FailureRatio: 1, Window: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}

	c := &clock{t: time.Unix(1_000_000, 0)}
	b.now = c.now

	gen, _, _ := b.Allow()
	b.Report(gen, true)

	// the first failure leaves the window
	c.t = c.t.Add(11 * time.Second)

	gen, _, _ = b.Allow()
	b.Report(gen, true)
	if b.state != closed {
		t.Fatal("old failure was counted")
	}

	gen, _, _ = b.Allow()
	b.Report(gen, true)
	if b.state != open {
		t.Fatal("not opened")
	}
}

func TestInvalidConfig(t *testing.T) {
	for _, c := range []Config{
		{FailureRatio: 1.5},
		{MinRequests: -1},
		{Window: -time.Second},
		{OpenDuration: -time.Second},
		{HalfOpenRequests: -1},
	} {
		if _, err := New("test", c); err == nil {
			t.Fatalf("%+v: expected error", c)
		}
	}
}
