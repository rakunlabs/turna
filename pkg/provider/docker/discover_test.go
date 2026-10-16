package docker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func fakeDocker(t *testing.T, routes map[string]any) *client {
	t.Helper()

	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/v1.47")

		v, ok := routes[path]
		if !ok {
			http.NotFound(w, r)

			return
		}

		_ = json.NewEncoder(w).Encode(v)
	}))
	t.Cleanup(s.Close)

	c, err := newClient(&Config{Endpoint: s.URL, APIVersion: "1.47"})
	if err != nil {
		t.Fatal(err)
	}

	return c
}

func container(name string, created int64, labels map[string]string, nets map[string]string, ports ...int) map[string]any {
	networks := map[string]any{}
	for n, ip := range nets {
		networks[n] = map[string]any{"IPAddress": ip}
	}

	ps := []any{}
	for _, p := range ports {
		ps = append(ps, map[string]any{"PrivatePort": p, "Type": "tcp"})
	}

	return map[string]any{
		"Id":              name + "0000000000000000",
		"Names":           []string{"/" + name},
		"Created":         created,
		"Labels":          labels,
		"State":           "running",
		"Status":          "Up 1 minute",
		"HostConfig":      map[string]any{"NetworkMode": "bridge"},
		"NetworkSettings": map[string]any{"Networks": networks},
		"Ports":           ps,
	}
}

func TestDiscoverContainers(t *testing.T) {
	compose := map[string]string{
		"turna.enable":               "true",
		"com.docker.compose.project": "shop",
		"com.docker.compose.service": "web",
	}

	unhealthy := container("shop-web-3", 3, compose, map[string]string{"shop_default": "10.0.0.4"}, 8080)
	unhealthy["Status"] = "Up 1 minute (unhealthy)"

	c := fakeDocker(t, map[string]any{
		"/containers/json": []any{
			container("shop-web-1", 1, compose, map[string]string{"shop_default": "10.0.0.2"}, 8080),
			container("shop-web-2", 2, compose, map[string]string{"shop_default": "10.0.0.3"}, 8080),
			unhealthy,
			container("plain", 5, map[string]string{"turna.enable": "true"}, map[string]string{"a": "10.1.0.2", "b": "10.2.0.2"}),
			container("ignored", 6, map[string]string{}, map[string]string{"bridge": "172.17.0.2"}),
		},
	})

	groups, err := discoverContainers(context.Background(), c, &Config{})
	if err != nil {
		t.Fatal(err)
	}

	byName := map[string]*group{}
	for _, g := range groups {
		byName[g.Name] = g
	}

	if len(byName) != 2 {
		t.Fatalf("groups %v", byName)
	}

	web := byName["shop/web"]
	if !reflect.DeepEqual(web.Targets, []string{"10.0.0.2", "10.0.0.3"}) || !reflect.DeepEqual(web.Ports["tcp"], []int{8080}) {
		t.Fatalf("shop-web %+v", web)
	}

	if byName["plain"].Err == nil || !strings.Contains(byName["plain"].Err.Error(), "several networks (a, b)") {
		t.Fatalf("plain err %v", byName["plain"].Err)
	}

	// network label, with compose project prefix
	groups, err = discoverContainers(context.Background(), c, &Config{Network: "default"})
	if err != nil {
		t.Fatal(err)
	}

	for _, g := range groups {
		if g.Name == "shop/web" && len(g.Targets) != 2 {
			t.Fatalf("targets %v", g.Targets)
		}
	}
}

func TestDiscoverSwarm(t *testing.T) {
	routes := map[string]any{
		"/services": []any{
			map[string]any{
				"ID":        "svc1",
				"CreatedAt": "2026-01-01T00:00:00Z",
				"Spec": map[string]any{
					"Name":   "stack_web",
					"Labels": map[string]string{"turna.enable": "true", "com.docker.stack.namespace": "stack"},
				},
				"Endpoint": map[string]any{
					"Ports": []any{map[string]any{"Protocol": "tcp", "TargetPort": 80}},
					"VirtualIPs": []any{
						map[string]any{"NetworkID": "ing", "Addr": "10.0.0.5/24"},
						map[string]any{"NetworkID": "app", "Addr": "10.10.0.5/24"},
					},
				},
			},
		},
		"/networks": []any{
			map[string]any{"Id": "ing", "Name": "ingress", "Ingress": true},
			map[string]any{"Id": "app", "Name": "stack_app"},
		},
		"/tasks": []any{
			task("t1", "svc1", "running", "10.10.0.7/24"),
			task("t2", "svc1", "running", "10.10.0.6/24"),
			task("t3", "svc1", "shutdown", "10.10.0.8/24"),
		},
	}

	c := fakeDocker(t, routes)

	groups, err := discoverSwarm(context.Background(), c, &Config{Swarm: Swarm{Enabled: true}})
	if err != nil {
		t.Fatal(err)
	}

	if len(groups) != 1 || groups[0].Err != nil {
		t.Fatalf("groups %+v", groups)
	}

	if !reflect.DeepEqual(groups[0].Targets, []string{"10.10.0.6", "10.10.0.7"}) || !reflect.DeepEqual(groups[0].Ports["tcp"], []int{80}) {
		t.Fatalf("group %+v", groups[0])
	}

	groups, err = discoverSwarm(context.Background(), c, &Config{Swarm: Swarm{Enabled: true, UseVIP: true}, Network: "app"})
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(groups[0].Targets, []string{"10.10.0.5"}) {
		t.Fatalf("vip targets %v err %v", groups[0].Targets, groups[0].Err)
	}
}

func task(id, service, state, addr string) map[string]any {
	return map[string]any{
		"ID":        id,
		"ServiceID": service,
		"Status":    map[string]any{"State": state},
		"NetworksAttachments": []any{
			map[string]any{"Network": map[string]any{"ID": "ing"}, "Addresses": []string{"10.0.0.9/24"}},
			map[string]any{"Network": map[string]any{"ID": "app"}, "Addresses": []string{addr}},
		},
	}
}
