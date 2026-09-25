package keylimit

import "testing"

func TestAllowPerKey(t *testing.T) {
	l := New(t.Context(), 0.001, 2)

	for i := range 2 {
		if !l.Allow("a") {
			t.Fatalf("a %d rejected", i)
		}
	}

	if l.Allow("a") {
		t.Fatal("a over burst allowed")
	}

	if !l.Allow("b") {
		t.Fatal("b should have its own bucket")
	}
}

func TestDefaultBurst(t *testing.T) {
	l := New(t.Context(), 0.5, 0)

	if !l.Allow("a") || l.Allow("a") {
		t.Fatal("default burst should be 1")
	}
}
