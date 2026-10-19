package docker

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rakunlabs/turna/pkg/server"
	shttp "github.com/rakunlabs/turna/pkg/server/http"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/hello"
)

func TestLabelTree(t *testing.T) {
	got, err := labelTree(map[string]string{
		"turna.enable":                       "true",
		"turna.http.routers.web.host":        "a.example.com",
		"turna.http.routers.web.middlewares": "strip",
		"other.label":                        "x",
	}, "turna")
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]any{
		"enable": "true",
		"http": map[string]any{
			"routers": map[string]any{
				"web": map[string]any{"host": "a.example.com", "middlewares": "strip"},
			},
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v", got)
	}

	if _, err := labelTree(map[string]string{"turna.a": "1", "turna.a.b": "2"}, "turna"); err == nil {
		t.Fatal("expected conflict error")
	}

	if _, err := labelTree(map[string]string{"turna.a..b": "1"}, "turna"); err == nil {
		t.Fatal("expected empty segment error")
	}
}

func TestReadLabels(t *testing.T) {
	lc, err := readLabels(map[string]string{
		"turna.http.routers.web.host":                                       "a.example.com, b.example.com",
		"turna.http.routers.web.path":                                       "/api/*",
		"turna.http.routers.web.tls":                                        "true",
		"turna.http.services.web.port":                                      "8080",
		"turna.http.services.web.pass_host_header":                          "false",
		"turna.http.services.web.health_check.interval":                     "5s",
		"turna.http.services.web.passive_health_check.fail_statuses":        "502,503",
		"turna.http.middlewares.strip.strip_prefix.prefixes.0":              "/api",
		"turna.http.middlewares.strip.strip_prefix.prefixes.1":              "/v1",
		"turna.http.middlewares.hdr.headers.custom_request_headers.X-Proxy": "turna",
	}, "turna")
	if err != nil {
		t.Fatal(err)
	}

	r := lc.HTTP.Routers["web"]
	if !reflect.DeepEqual(r.Host, []string{"a.example.com", "b.example.com"}) || !r.TLS {
		t.Fatalf("router %+v", r)
	}

	s := lc.HTTP.Services["web"]
	if s.Port != 8080 || s.PassHostHeader == nil || *s.PassHostHeader || s.HealthCheck.Interval != 5*time.Second {
		t.Fatalf("service %+v", s)
	}

	if !reflect.DeepEqual(s.PassiveHealthCheck.FailStatuses, []int{502, 503}) {
		t.Fatalf("fail statuses %v", s.PassiveHealthCheck.FailStatuses)
	}

	if _, err := readLabels(map[string]string{"turna.http.routers.web.hots": "x"}, "turna"); err == nil {
		t.Fatal("expected error for unknown key")
	}
}

func staticServer() *server.Server {
	return &server.Server{
		EntryPoints: map[string]server.EntryPoint{
			"web":       {Address: ":80"},
			"websecure": {Address: ":443"},
			"db":        {Address: ":5432"},
			"dns":       {Address: ":53", Network: "udp"},
		},
		HTTP: shttp.HTTP{
			Routers: map[string]shttp.Router{
				"static": {Host: "static.example.com", Path: []string{"/*"}, EntryPoints: []string{"web"}, Middlewares: []string{"auth"}},
				"secure": {Host: "secure.example.com", Path: []string{"/*"}, EntryPoints: []string{"websecure"}, TLS: &struct{}{}},
			},
			Middlewares: map[string]shttp.HTTPMiddleware{"auth": {HelloMiddleware: &hello.Hello{}}},
		},
	}
}

func TestTranslateHTTP(t *testing.T) {
	cfg := &Config{HTTP: Protocol{StaticMiddlewares: []string{"auth"}}}
	g := &group{
		Name:    "shop-web",
		Targets: []string{"10.0.0.2", "10.0.0.3"},
		Ports:   map[string][]int{"tcp": {8080}},
		Labels: map[string]string{
			"turna.http.routers.web.host":                          "shop.example.com",
			"turna.http.routers.web.entrypoints":                   "web",
			"turna.http.routers.web.middlewares":                   "strip,auth",
			"turna.http.middlewares.strip.strip-prefix.prefixes.0": "/api",
		},
	}

	f, err := translate(cfg, g, newClaims(staticServer()))
	if err != nil {
		t.Fatal(err)
	}

	if len(f.warnings) > 0 {
		t.Fatal(f.warnings)
	}

	router, ok := f.server.HTTP.Routers["docker@shop-web@http@web"]
	if !ok {
		t.Fatalf("routers %v", keys(f.server.HTTP.Routers))
	}

	wantMW := []string{"docker@shop-web@http@strip", "auth", "docker@shop-web@http-service@default"}
	if !reflect.DeepEqual(router.Middlewares, wantMW) {
		t.Fatalf("middlewares %v", router.Middlewares)
	}

	svc := f.server.HTTP.Middlewares["docker@shop-web@http-service@default"].ServiceMiddleware
	if svc == nil || len(svc.LoadBalancer.Servers) != 2 || svc.LoadBalancer.Servers[0].URL != "http://10.0.0.2:8080" {
		t.Fatalf("service %+v", svc)
	}

	if svc.Retry != defaultRetry {
		t.Fatalf("retry %d", svc.Retry)
	}

	if f.server.HTTP.Middlewares["docker@shop-web@http@strip"].StripPrefixMiddleware == nil {
		t.Fatal("strip_prefix middleware missing")
	}
}

func TestTranslateDefaultEntrypoints(t *testing.T) {
	g := &group{
		Name:    "app",
		Targets: []string{"10.0.0.2"},
		Ports:   map[string][]int{"tcp": {80}},
		Labels:  map[string]string{"turna.http.routers.app.host": "app.example.com"},
	}

	f, err := translate(&Config{}, g, newClaims(staticServer()))
	if err != nil {
		t.Fatal(err)
	}

	// plain routers avoid the tls entrypoint
	if eps := f.server.HTTP.Routers["docker@app@http@app"].EntryPoints; !reflect.DeepEqual(eps, []string{"db", "web"}) {
		t.Fatalf("entrypoints %v", eps)
	}

	f, err = translate(&Config{HTTP: Protocol{EntryPoints: []string{"web"}}}, g, newClaims(staticServer()))
	if err != nil {
		t.Fatal(err)
	}

	if eps := f.server.HTTP.Routers["docker@app@http@app"].EntryPoints; !reflect.DeepEqual(eps, []string{"web"}) {
		t.Fatalf("entrypoints %v", eps)
	}
}

func TestTranslateRejects(t *testing.T) {
	base := map[string]string{"turna.http.routers.r.entrypoints": "web"}

	tests := []struct {
		name   string
		cfg    Config
		labels map[string]string
		ports  map[string][]int
		err    string
		warn   string
	}{
		{
			name:   "middleware type not allowed",
			labels: map[string]string{"turna.http.middlewares.f.folder.path": "/etc"},
			err:    "type folder is not allowed",
		},
		{
			name:   "middleware type allowed by config",
			cfg:    Config{HTTP: Protocol{Middlewares: []string{"*"}}},
			labels: map[string]string{"turna.http.middlewares.f.folder.path": "/srv"},
		},
		{
			name:   "unknown middleware type",
			labels: map[string]string{"turna.http.middlewares.f.nope.x": "1"},
			err:    "unknown type nope",
		},
		{
			name:   "two middleware types",
			labels: map[string]string{"turna.http.middlewares.f.gzip": "", "turna.http.middlewares.f.decompress": ""},
			err:    "exactly one type",
		},
		{
			name:   "rate limit redis in labels",
			labels: map[string]string{"turna.http.middlewares.f.rate_limit.redis.address": "evil:6379"},
			err:    "rate_limit redis is not allowed",
		},
		{
			name: "new filter middlewares allowed",
			labels: map[string]string{
				"turna.http.middlewares.f.ip_filter.allow":     "10.0.0.0/8,192.168.0.0/16",
				"turna.http.middlewares.b.body_limit.max_size": "1MB",
				"turna.http.middlewares.t.timeout.duration":    "30s",
			},
		},
		{
			name:   "unknown middleware reference",
			labels: map[string]string{"turna.http.routers.r.middlewares": "missing"},
			warn:   "middleware missing is not defined",
		},
		{
			name:   "static middleware not allowed",
			labels: map[string]string{"turna.http.routers.r.middlewares": "auth"},
			warn:   "middleware auth is not allowed in labels",
		},
		{
			name:   "static host with another path",
			labels: map[string]string{"turna.http.routers.r.host": "static.example.com", "turna.http.routers.r.path": "/admin/*"},
			warn:   "host static.example.com is served by the static configuration",
		},
		{
			name:  "several ports",
			ports: map[string][]int{"tcp": {80, 443}},
			warn:  "several ports are exposed",
		},
		{
			name:   "route of other layer",
			labels: map[string]string{"turna.http.routers.r.host": "static.example.com"},
			warn:   "host static.example.com is served by the static configuration",
		},
		{
			name:   "tls mismatch",
			labels: map[string]string{"turna.http.routers.r.entrypoints": "websecure"},
			warn:   "entrypoint websecure serves tls",
		},
		{
			name:   "unknown entrypoint",
			labels: map[string]string{"turna.http.routers.r.entrypoints": "nope"},
			warn:   "entrypoint nope does not exist",
		},
		{
			name:   "entrypoint not allowed",
			cfg:    Config{HTTP: Protocol{EntryPoints: []string{"websecure"}}},
			labels: map[string]string{},
			warn:   "entrypoint web is not allowed",
		},
		{
			name:   "tcp entrypoint not allowed",
			labels: map[string]string{"turna.tcp.routers.db.entrypoints": "db"},
			warn:   "entrypoint db is not allowed",
		},
		{
			name:   "name with @",
			labels: map[string]string{"turna.http.routers.a@b.host": "x"},
			err:    "must not contain @",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			labels := map[string]string{}
			for k, v := range base {
				labels[k] = v
			}
			for k, v := range tt.labels {
				labels[k] = v
			}

			ports := tt.ports
			if ports == nil {
				ports = map[string][]int{"tcp": {80}}
			}

			g := &group{Name: "g", Targets: []string{"10.0.0.2"}, Ports: ports, Labels: labels}

			f, err := translate(&tt.cfg, g, newClaims(staticServer()))
			if tt.err != "" {
				if err == nil || !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("got error %v, want %q", err, tt.err)
				}

				return
			}

			if err != nil {
				t.Fatal(err)
			}

			var warnings []string
			for _, w := range f.warnings {
				warnings = append(warnings, w.Error())
			}

			if tt.warn == "" {
				if len(warnings) > 0 {
					t.Fatalf("unexpected warnings %v", warnings)
				}

				return
			}

			if !strings.Contains(strings.Join(warnings, "\n"), tt.warn) {
				t.Fatalf("got warnings %v, want %q", warnings, tt.warn)
			}
		})
	}
}

func TestTranslateAllOlderWins(t *testing.T) {
	now := time.Now()
	labels := map[string]string{"turna.http.routers.r.host": "app.example.com", "turna.http.routers.r.entrypoints": "web"}

	groups := []*group{
		{Name: "new", Created: now, Labels: labels, Targets: []string{"10.0.0.3"}, Ports: map[string][]int{"tcp": {80}}},
		{Name: "old", Created: now.Add(-time.Hour), Labels: labels, Targets: []string{"10.0.0.2"}, Ports: map[string][]int{"tcp": {80}}},
	}

	fragments, errs := translateAll(&Config{}, groups, staticServer())
	if len(errs) > 0 {
		t.Fatal(errs)
	}

	if len(fragments["old"].server.HTTP.Routers) != 1 {
		t.Fatal("older group lost its route")
	}

	if len(fragments["new"].server.HTTP.Routers) != 0 || len(fragments["new"].warnings) != 1 {
		t.Fatalf("newer group: routers %v warnings %v", fragments["new"].server.HTTP.Routers, fragments["new"].warnings)
	}
}

func TestTranslateStreams(t *testing.T) {
	cfg := &Config{TCP: Protocol{EntryPoints: []string{"db"}}, UDP: Protocol{EntryPoints: []string{"dns"}}}

	g := &group{
		Name:    "infra",
		Targets: []string{"10.0.0.2"},
		Ports:   map[string][]int{"tcp": {5432}, "udp": {53}},
		Labels: map[string]string{
			"turna.tcp.routers.pg.entrypoints":                       "db",
			"turna.tcp.routers.pg.middlewares":                       "local",
			"turna.tcp.middlewares.local.ip_allow_list.source_range": "10.0.0.0/8",
			"turna.udp.routers.dns.entrypoints":                      "dns",
		},
	}

	f, err := translate(cfg, g, newClaims(staticServer()))
	if err != nil {
		t.Fatal(err)
	}

	if len(f.warnings) > 0 {
		t.Fatal(f.warnings)
	}

	pg := f.server.TCP.Routers["docker@infra@tcp@pg"]
	if !reflect.DeepEqual(pg.Middlewares, []string{"docker@infra@tcp@local", "docker@infra@tcp-service@default"}) {
		t.Fatalf("tcp router %+v", pg)
	}

	lb := f.server.TCP.Middlewares["docker@infra@tcp-service@default"].LoadBalancerMiddleware
	if lb == nil || lb.Servers[0].Address != "10.0.0.2:5432" {
		t.Fatalf("tcp lb %+v", lb)
	}

	ulb := f.server.UDP.Middlewares["docker@infra@udp-service@default"].LoadBalancerMiddleware
	if ulb == nil || ulb.Servers[0].Address != "10.0.0.2:53" {
		t.Fatalf("udp lb %+v", ulb)
	}

	// a second group cannot take the same entrypoint
	g2 := &group{Name: "other", Targets: []string{"10.0.0.3"}, Ports: map[string][]int{"tcp": {5432}}, Labels: map[string]string{"turna.tcp.routers.pg.entrypoints": "db"}}

	cl := newClaims(staticServer())
	if _, err := translate(cfg, g, cl); err != nil {
		t.Fatal(err)
	}

	f2, err := translate(cfg, g2, cl)
	if err != nil {
		t.Fatal(err)
	}

	if len(f2.server.TCP.Routers) != 0 || len(f2.warnings) == 0 {
		t.Fatal("second tcp router took a used entrypoint")
	}
}

func TestTranslateHTTPKeepsTCPEntrypoints(t *testing.T) {
	cfg := &Config{TCP: Protocol{EntryPoints: []string{"db"}}}

	g := &group{
		Name:    "app",
		Targets: []string{"10.0.0.2"},
		Ports:   map[string][]int{"tcp": {8080}},
		Labels: map[string]string{
			"turna.http.routers.web.host":      "app.example.com",
			"turna.http.services.web.port":     "8080",
			"turna.tcp.routers.pg.entrypoints": "db",
			"turna.tcp.routers.pg.service":     "pg",
			"turna.tcp.services.pg.port":       "5432",
		},
	}

	f, err := translate(cfg, g, newClaims(staticServer()))
	if err != nil {
		t.Fatal(err)
	}

	if len(f.warnings) > 0 {
		t.Fatal(f.warnings)
	}

	if eps := f.server.HTTP.Routers["docker@app@http@web"].EntryPoints; slices.Contains(eps, "db") {
		t.Fatalf("http router took tcp entrypoint: %v", eps)
	}

	if _, ok := f.server.TCP.Routers["docker@app@tcp@pg"]; !ok {
		t.Fatal("tcp router missing")
	}
}

func TestTranslatePrunesUnusedServices(t *testing.T) {
	g := &group{
		Name:    "app",
		Targets: []string{"10.0.0.2"},
		Ports:   map[string][]int{"tcp": {8080}},
		Labels: map[string]string{
			// the static configuration serves this host
			"turna.http.routers.web.host":          "static.example.com",
			"turna.http.services.web.health_check": "true",
		},
	}

	f, err := translate(&Config{}, g, newClaims(staticServer()))
	if err != nil {
		t.Fatal(err)
	}

	if len(f.warnings) == 0 {
		t.Fatal("expected a warning")
	}

	if len(f.server.HTTP.Middlewares) != 0 {
		t.Fatalf("unused service kept: %v", keys(f.server.HTTP.Middlewares))
	}
}

func TestEnabled(t *testing.T) {
	if enabled(map[string]string{}, "turna", false) {
		t.Fatal("enabled without label")
	}

	if !enabled(map[string]string{}, "turna", true) {
		t.Fatal("not enabled by default")
	}

	if enabled(map[string]string{"turna.enable": "false"}, "turna", true) {
		t.Fatal("enable=false ignored")
	}

	if !enabled(map[string]string{"turna.enable": "true"}, "turna", false) {
		t.Fatal("enable=true ignored")
	}
}
