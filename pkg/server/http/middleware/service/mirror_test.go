package service

import (
	"net/http"
	"testing"
)

func TestMirrorSelectsOneServiceEndpoint(t *testing.T) {
	m := &Mirror{Servers: []MirrorServer{{Endpoints: []string{"http://one:80", "http://two:80"}}}}
	mr, err := m.build(http.DefaultTransport, make(unixSockets))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for range 1000 {
		targets := mr.selectTargets()
		if len(targets) != 1 {
			t.Fatalf("100%% mirror selected %d endpoints", len(targets))
		}
		seen[targets[0].Host] = true
	}
	if len(seen) != 2 {
		t.Fatal("selection never used one of the endpoints")
	}
}

func TestMirrorIndependentTargets(t *testing.T) {
	zero := 0.0
	m := &Mirror{Servers: []MirrorServer{
		{URL: "http://one:80"},
		{Endpoints: []string{"http://two:80", "http://three:80"}},
		{Endpoints: []string{"http://disabled:80"}, Percent: &zero},
	}}
	mr, err := m.build(http.DefaultTransport, make(unixSockets))
	if err != nil {
		t.Fatal(err)
	}
	for range 100 {
		selected := mr.selectTargets()
		if len(selected) != 2 || selected[0].Host != "one:80" {
			t.Fatalf("selected = %v", selected)
		}
	}
}

func TestMirrorPercentageSelectsAtMostOneEndpoint(t *testing.T) {
	percent := 50.0
	m := &Mirror{Servers: []MirrorServer{{Endpoints: []string{"http://one:80", "http://two:80"}, Percent: &percent}}}
	mr, err := m.build(http.DefaultTransport, make(unixSockets))
	if err != nil {
		t.Fatal(err)
	}
	var mirrored int
	for range 1000 {
		selected := mr.selectTargets()
		if len(selected) > 1 {
			t.Fatal("more than one service endpoint selected")
		}
		mirrored += len(selected)
	}
	if mirrored < 300 || mirrored > 700 {
		t.Fatalf("unexpected 50%% sample: %d/1000", mirrored)
	}
}
