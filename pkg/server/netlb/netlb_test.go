package netlb

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestWeightedRoundRobin(t *testing.T) {
	b, err := New("", []Server{{Address: "a", Weight: 5}, {Address: "b", Weight: 1}, {Address: "c", Weight: 1}}, nil)
	if err != nil {
		t.Fatal(err)
	}

	var seq string
	for range 7 {
		tg, _ := b.Next(nil)
		seq += tg.Address
	}

	// smooth weighted round-robin of nginx
	if seq != "aabacaa" {
		t.Fatalf("sequence = %s", seq)
	}
}

func TestLeastConnAndExclude(t *testing.T) {
	b, _ := New(StrategyLeastConn, []Server{{Address: "a"}, {Address: "b"}}, nil)

	a, _ := b.Next(nil)
	release := b.Acquire(a)

	if next, _ := b.Next(nil); next == a {
		t.Fatal("least_conn picked the busy target")
	}

	release()
	release() // idempotent

	if a.Active() != 0 {
		t.Fatalf("active = %d", a.Active())
	}

	other, _ := b.Next([]*Target{a})
	if other == a {
		t.Fatal("excluded target returned")
	}

	if _, err := b.Next(b.Targets()); !errors.Is(err, ErrNoTarget) {
		t.Fatalf("err = %v", err)
	}
}

func TestPassiveEjectionAndFailOpen(t *testing.T) {
	b, _ := New(StrategyRandom, []Server{{Address: "a"}, {Address: "b"}}, &PassiveHealthCheck{MaxFails: 2, FailTimeout: time.Minute})
	a, bt := b.Targets()[0], b.Targets()[1]

	b.ReportFailure(a)
	b.ReportFailure(a)

	for range 20 {
		if tg, _ := b.Next(nil); tg != bt {
			t.Fatal("ejected target picked")
		}
	}

	b.ReportFailure(bt)
	b.ReportFailure(bt)

	// all ejected: fail open
	if tg, err := b.Next(nil); err != nil || tg == nil {
		t.Fatalf("fail open: %v", err)
	}
}

func TestActiveHealthCheck(t *testing.T) {
	b, _ := New("", []Server{{Address: "down"}, {Address: "up"}}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	b.StartHealthCheck(ctx, HealthCheck{Interval: 10 * time.Millisecond}, func(_ context.Context, address string) error {
		if address == "down" {
			return errors.New("down")
		}

		return nil
	})

	deadline := time.Now().Add(2 * time.Second)
	for b.Targets()[0].healthy.Load() {
		if time.Now().After(deadline) {
			t.Fatal("target not marked unhealthy")
		}

		time.Sleep(5 * time.Millisecond)
	}

	for range 5 {
		if tg, _ := b.Next(nil); tg.Address != "up" {
			t.Fatal("unhealthy target picked")
		}
	}
}

func TestInvalid(t *testing.T) {
	for _, tc := range []struct {
		strategy string
		servers  []Server
	}{
		{"fastest", []Server{{Address: "a"}}},
		{"", nil},
		{"", []Server{{Address: ""}}},
		{"", []Server{{Address: "a", Weight: -1}}},
	} {
		if _, err := New(tc.strategy, tc.servers, nil); err == nil {
			t.Errorf("%+v: expected error", tc)
		}
	}
}
