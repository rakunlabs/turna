package docker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/rakunlabs/turna/pkg/server"
	shttp "github.com/rakunlabs/turna/pkg/server/http"
)

func TestProviderApplyReleasesClaimsOfDroppedGroups(t *testing.T) {
	m := newManager(t)

	if err := m.SetLayer(server.LayerStatic, &server.Server{
		EntryPoints: map[string]server.EntryPoint{"web": {Address: "127.0.0.1:0"}},
	}); err != nil {
		t.Fatal(err)
	}

	addr, _ := m.Addr("web")
	ip, port := backend(t, "young")

	p := &Provider{applier: m, reported: map[string]string{}}

	groups := []*group{
		{
			// older, claims the host first but fails when the server builds it
			Name: "broken", Created: time.Unix(1, 0), Targets: []string{ip}, Ports: map[string][]int{"tcp": {port}},
			Labels: map[string]string{
				"turna.http.routers.r.host":      "app.test",
				"turna.http.routers.r.service":   "s",
				"turna.http.services.s.strategy": "nope",
			},
		},
		{
			Name: "young", Created: time.Unix(2, 0), Targets: []string{ip}, Ports: map[string][]int{"tcp": {port}},
			Labels: map[string]string{"turna.http.routers.r.host": "app.test"},
		},
	}

	for range 2 {
		fragments, errs := p.apply(groups, withoutOwned(m.Current()))
		if _, ok := errs["broken"]; !ok || len(errs) != 1 {
			t.Fatalf("errors %v", errs)
		}

		if _, ok := fragments["young"]; !ok {
			t.Fatalf("fragments %v", keys(fragments))
		}

		if code, body := get(t, addr.String(), "app.test"); code != 200 || body != "young" {
			t.Fatalf("app.test: %d %q", code, body)
		}
	}
}

func TestEventsReady(t *testing.T) {
	release := make(chan struct{})

	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/fail/events") {
			http.Error(w, "nope", http.StatusInternalServerError)

			return
		}

		<-release
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer s.Close()
	defer close(release)

	c, err := newClient(&Config{Endpoint: s.URL, APIVersion: "1.47"})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ready := make(chan struct{})
	done := make(chan error, 1)

	go func() { done <- c.events(ctx, []string{"container"}, ready, func(apiEvent) {}) }()

	select {
	case <-ready:
		t.Fatal("ready before the stream is open")
	case <-time.After(50 * time.Millisecond):
	}

	release <- struct{}{}

	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("not ready after the stream is open")
	}

	cancel()
	<-done

	c.version = "1.47/fail"
	failReady := make(chan struct{})

	if err := c.events(context.Background(), []string{"container"}, failReady, func(apiEvent) {}); err == nil {
		t.Fatal("expected error")
	}

	select {
	case <-failReady:
		t.Fatal("ready on failed stream")
	default:
	}
}

func TestTLSFromEnvironment(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"ca.pem", "cert.pem", "key.pem"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	t.Setenv("DOCKER_TLS_VERIFY", "")

	if got := (&Config{}).tls(); got != nil {
		t.Fatalf("tls without env %+v", got)
	}

	t.Setenv("DOCKER_TLS_VERIFY", "1")
	t.Setenv("DOCKER_CERT_PATH", dir)

	want := &TLS{CA: filepath.Join(dir, "ca.pem"), Cert: filepath.Join(dir, "cert.pem"), Key: filepath.Join(dir, "key.pem")}
	if got := (&Config{}).tls(); !reflect.DeepEqual(got, want) {
		t.Fatalf("tls %+v", got)
	}

	explicit := &TLS{InsecureSkipVerify: true}
	if got := (&Config{TLS: explicit}).tls(); got != explicit {
		t.Fatalf("explicit tls replaced %+v", got)
	}

	t.Setenv("DOCKER_CERT_PATH", t.TempDir())

	c, err := newClient(&Config{Endpoint: "tcp://docker.test:2376"})
	if err != nil {
		t.Fatal(err)
	}

	if c.base != "https://docker.test:2376" {
		t.Fatalf("base %s", c.base)
	}
}

func TestDiscoverKeepsGroupWithoutHealthyReplicas(t *testing.T) {
	labels := map[string]string{
		"turna.enable":               "true",
		"turna.http.routers.r.host":  "down.test",
		"com.docker.compose.project": "shop",
		"com.docker.compose.service": "web",
	}

	starting := container("shop-web-1", 1, labels, map[string]string{"shop_default": "10.0.0.2"}, 8080)
	starting["Status"] = "Up 1 second (health: starting)"

	c := fakeDocker(t, map[string]any{"/containers/json": []any{starting}})

	groups, err := discoverContainers(context.Background(), c, &Config{})
	if err != nil {
		t.Fatal(err)
	}

	if len(groups) != 1 || len(groups[0].Targets) != 0 || groups[0].Err != nil {
		t.Fatalf("groups %+v", groups)
	}

	m := newManager(t)
	if err := m.SetLayer(server.LayerStatic, &server.Server{
		EntryPoints: map[string]server.EntryPoint{"web": {Address: "127.0.0.1:0"}},
	}); err != nil {
		t.Fatal(err)
	}

	addr, _ := m.Addr("web")

	p := &Provider{applier: m, reported: map[string]string{}}
	if _, errs := p.apply(groups, withoutOwned(m.Current())); len(errs) > 0 {
		t.Fatal(errs)
	}

	if code, _ := get(t, addr.String(), "down.test"); code != http.StatusServiceUnavailable {
		t.Fatalf("down.test: %d", code)
	}
}

func TestTranslateConfiguredEntrypointsFilteredByTLS(t *testing.T) {
	g := &group{
		Name:    "app",
		Targets: []string{"10.0.0.2"},
		Ports:   map[string][]int{"tcp": {80}},
		Labels:  map[string]string{"turna.http.routers.app.host": "app.example.com"},
	}

	cfg := &Config{HTTP: Protocol{EntryPoints: []string{"web", "websecure"}}}

	f, err := translate(cfg, g, newClaims(staticServer()))
	if err != nil {
		t.Fatal(err)
	}

	if len(f.warnings) > 0 {
		t.Fatal(f.warnings)
	}

	if eps := f.server.HTTP.Routers["docker@app@http@app"].EntryPoints; !reflect.DeepEqual(eps, []string{"web"}) {
		t.Fatalf("entrypoints %v", eps)
	}

	g.Labels["turna.http.routers.app.tls"] = "true"

	f, err = translate(cfg, g, newClaims(staticServer()))
	if err != nil {
		t.Fatal(err)
	}

	if eps := f.server.HTTP.Routers["docker@app@http@app"].EntryPoints; !reflect.DeepEqual(eps, []string{"websecure"}) {
		t.Fatalf("tls entrypoints %v", eps)
	}
}

func TestClaimsMixedTLSEntrypoint(t *testing.T) {
	s := staticServer()
	s.HTTP.Routers["plain-on-secure"] = shttp.Router{Host: "other.example.com", EntryPoints: []string{"websecure"}}

	for range 20 {
		cl := newClaims(s)

		for _, tls := range []bool{true, false} {
			if err := cl.claimHTTP("x.example.com", []string{"/*"}, []string{"websecure"}, tls); err == nil || !strings.Contains(err.Error(), "both tls and plain") {
				t.Fatalf("tls=%v err %v", tls, err)
			}
		}

		if eps := cl.defaultEntrypoints(true, nil); !reflect.DeepEqual(eps, []string{"db"}) {
			t.Fatalf("default tls entrypoints %v", eps)
		}
	}
}

func TestPortErrorMessages(t *testing.T) {
	for _, tt := range []struct {
		g    group
		want string
	}{
		{group{Swarm: true}, "swarm only lists published ports"},
		{group{HostNetwork: true}, "host network containers list no ports"},
		{group{}, "no port is exposed"},
	} {
		tr := &translator{g: &tt.g}
		if _, err := tr.port("http", "default", 0); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Fatalf("err %v, want %q", err, tt.want)
		}
	}
}
