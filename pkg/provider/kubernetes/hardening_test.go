package kubernetes

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kubefake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
	gwfake "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned/fake"

	"github.com/rakunlabs/turna/pkg/server/cert"
)

func tlsSecret(t *testing.T, ns, name, host string) *corev1.Secret {
	t.Helper()

	generated, err := cert.GenerateCertificate(cert.WithDNSNames(host))
	if err != nil {
		t.Fatal(err)
	}

	return &corev1.Secret{
		ObjectMeta: objMeta(ns, name),
		Type:       corev1.SecretTypeTLS,
		Data:       map[string][]byte{corev1.TLSCertKey: generated.Certificate, corev1.TLSPrivateKeyKey: generated.PrivateKey},
	}
}

func httpsListener(name, host string, refs ...gwv1.SecretObjectReference) gwv1.Listener {
	return gwv1.Listener{
		Name: gwv1.SectionName(name), Port: 443, Protocol: gwv1.HTTPSProtocolType,
		Hostname: ptr(gwv1.Hostname(host)),
		TLS:      &gwv1.ListenerTLSConfig{CertificateRefs: refs},
	}
}

// Status that already matches the snapshot must not cost API requests.
func TestStatusUnchangedSkipsRequests(t *testing.T) {
	gc := class()
	gw := gatewayObj("app", "gw", httpListener("http", 80, ""))
	route := routeObj("app", "r", "gw", nil, gwv1.HTTPRouteRule{})
	snap := &Snapshot{GatewayClasses: []*gwv1.GatewayClass{gc}, Gateways: []*gwv1.Gateway{gw}, HTTPRoutes: []*gwv1.HTTPRoute{route}}

	client := gwfake.NewSimpleClientset(gc, route)
	if _, err := client.GatewayV1().Gateways("app").Create(context.Background(), gw, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}

	p := NewWithClients(Config{Status: Status{Addresses: []string{"192.0.2.1"}}}, nopApplier{}, kubefake.NewSimpleClientset(), client)
	w := newStatusWriter(p)
	res := Translate(&p.cfg, snap)

	w.write(context.Background(), &statusUpdate{snap: snap, res: res})

	// the informers deliver the written status with the next snapshot
	next := &Snapshot{}
	gcNow, _ := client.GatewayV1().GatewayClasses().Get(context.Background(), gc.Name, metav1.GetOptions{})
	gwNow, _ := client.GatewayV1().Gateways("app").Get(context.Background(), "gw", metav1.GetOptions{})
	routeNow, _ := client.GatewayV1().HTTPRoutes("app").Get(context.Background(), "r", metav1.GetOptions{})
	next.GatewayClasses = []*gwv1.GatewayClass{gcNow}
	next.Gateways = []*gwv1.Gateway{gwNow}
	next.HTTPRoutes = []*gwv1.HTTPRoute{routeNow}

	client.ClearActions()
	w.write(context.Background(), &statusUpdate{snap: next, res: Translate(&p.cfg, next)})

	if actions := client.Actions(); len(actions) != 0 {
		t.Fatalf("unchanged status made %d requests: %v", len(actions), actions)
	}
}

// The controller reports status only and does not need endpoints; a change
// in an unrelated EndpointSlice must not trigger a sync.
func TestControllerDoesNotWatchEndpointSlices(t *testing.T) {
	t.Setenv("POD_NAMESPACE", "turna-system")

	kube := kubefake.NewSimpleClientset()
	gw := gwfake.NewSimpleClientset()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	p := NewWithClients(Config{
		Mode:     ModeController,
		Debounce: time.Millisecond,
		Status:   Status{LeaderElection: LeaderElection{Disabled: true}},
		Proxy:    Proxy{Image: "turna:test"},
	}, nopApplier{}, kube, gw)

	go func() { _ = p.Run(ctx) }()

	select {
	case <-p.Ready():
	case <-time.After(5 * time.Second):
		t.Fatal("controller not ready")
	}

	for _, a := range kube.Actions() {
		if a.GetResource().Resource == "endpointslices" {
			t.Fatalf("controller watches endpointslices: %v", a)
		}
	}
}

// Proxy pods must not run on a single node and must keep one replica
// during voluntary disruptions.
func TestProxyAvailability(t *testing.T) {
	ctx := context.Background()
	kube := kubefake.NewSimpleClientset()
	p := &Provider{kube: kube, cfg: Config{Proxy: Proxy{Namespace: "system", Image: "example:1", Replicas: 2}}}
	gw := gatewayObj("demo", "web", httpListener("http", 80, ""))
	name := proxyName(key(gw))

	if err := p.provision(ctx, []*gwv1.Gateway{gw}, nil, nil); err != nil {
		t.Fatal(err)
	}

	pdb, err := kube.PolicyV1().PodDisruptionBudgets("system").Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("proxy pdb not created: %v", err)
	}
	if pdb.Spec.MaxUnavailable == nil || pdb.Spec.MaxUnavailable.IntValue() != 1 ||
		pdb.Spec.Selector.MatchLabels["app.kubernetes.io/instance"] != proxyLabels(key(gw))["app.kubernetes.io/instance"] {
		t.Fatalf("pdb = %+v", pdb.Spec)
	}

	dep, err := kube.AppsV1().Deployments("system").Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	spec := dep.Spec.Template.Spec
	if len(spec.TopologySpreadConstraints) != 2 || spec.TopologySpreadConstraints[0].TopologyKey != corev1.LabelHostname {
		t.Fatalf("topology spread = %+v", spec.TopologySpreadConstraints)
	}
	if lc := spec.Containers[0].Lifecycle; lc == nil || lc.PreStop == nil || lc.PreStop.Sleep == nil || lc.PreStop.Sleep.Seconds != 5 {
		t.Fatalf("preStop = %+v", lc)
	}
	if ptrOr(spec.TerminationGracePeriodSeconds, 0) < 20 {
		t.Fatalf("grace period %d is shorter than preStop and shutdown", ptrOr(spec.TerminationGracePeriodSeconds, 0))
	}

	// a single replica must not block node drains
	p.cfg.Proxy.Replicas = 1
	if err := p.provision(ctx, []*gwv1.Gateway{gw}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := kube.PolicyV1().PodDisruptionBudgets("system").Get(ctx, name, metav1.GetOptions{}); err == nil {
		t.Fatal("pdb of a single replica proxy not removed")
	}

	// removing the gateway removes its budget
	p.cfg.Proxy.Replicas = 2
	if err := p.provision(ctx, []*gwv1.Gateway{gw}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := p.provision(ctx, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := kube.PolicyV1().PodDisruptionBudgets("system").Get(ctx, name, metav1.GetOptions{}); err == nil {
		t.Fatal("pdb of a removed gateway not deleted")
	}
}

// Proxies get only the certificates of their own gateway.
func TestProxyCertificateBundle(t *testing.T) {
	ctx := context.Background()
	kube := kubefake.NewSimpleClientset()
	p := &Provider{kube: kube, cfg: Config{Proxy: Proxy{Namespace: "system", Image: "example:1"}}}

	own := tlsSecret(t, "demo", "own", "a.example.com")
	shared := tlsSecret(t, "certs", "shared", "b.example.com")
	unrelated := tlsSecret(t, "demo", "unrelated", "c.example.com")
	denied := tlsSecret(t, "other", "denied", "d.example.com")

	grant := &gwv1.ReferenceGrant{
		ObjectMeta: objMeta("certs", "allow"),
		Spec: gwv1.ReferenceGrantSpec{
			From: []gwv1.ReferenceGrantFrom{{Group: groupGateway, Kind: kindGateway, Namespace: "demo"}},
			To:   []gwv1.ReferenceGrantTo{{Kind: kindSecret}},
		},
	}

	gw := gatewayObj("demo", "web",
		httpsListener("a", "a.example.com", gwv1.SecretObjectReference{Name: "own"}),
		httpsListener("b", "b.example.com", gwv1.SecretObjectReference{Name: "shared", Namespace: ptr(gwv1.Namespace("certs"))}),
		httpsListener("d", "d.example.com", gwv1.SecretObjectReference{Name: "denied", Namespace: ptr(gwv1.Namespace("other"))}),
	)

	snap := &Snapshot{Secrets: []*corev1.Secret{own, shared, unrelated, denied}, ReferenceGrants: []*gwv1.ReferenceGrant{grant}}
	if err := p.provision(ctx, []*gwv1.Gateway{gw}, snap, nil); err != nil {
		t.Fatal(err)
	}

	name := proxyName(key(gw))
	bundle, err := kube.CoreV1().Secrets("system").Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}

	var got []string
	for _, s := range unbundleSecrets(bundle) {
		got = append(got, key(s).String())
	}
	slices.Sort(got)

	if want := []string{"certs/shared", "demo/own"}; !slices.Equal(got, want) {
		t.Fatalf("bundled secrets = %v, want %v", got, want)
	}

	cm, err := kube.CoreV1().ConfigMaps("system").Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if want := "certificate_secret: system/" + name; !strings.Contains(cm.Data["turna.yaml"], want) {
		t.Fatalf("proxy config does not reference the bundle:\n%s", cm.Data["turna.yaml"])
	}

	// the proxy translates the gateway from the bundle alone
	res := Translate(&Config{Mode: ModeProxy, Gateways: []string{"demo/web"}}, &Snapshot{
		GatewayClasses:  []*gwv1.GatewayClass{class()},
		Gateways:        []*gwv1.Gateway{gw},
		ReferenceGrants: []*gwv1.ReferenceGrant{grant},
		Secrets:         unbundleSecrets(bundle),
	})
	st := res.Gateways[key(gw)]
	for i, want := range []bool{true, true, false} {
		c := condition(t, st.Listeners[i].Conditions, string(gwv1.ListenerConditionProgrammed))
		if (c.Status == metav1.ConditionTrue) != want {
			t.Errorf("listener %s programmed = %s (%s)", st.Listeners[i].Name, c.Status, c.Message)
		}
	}
}

// A proxy configured with a certificate bundle must not watch Secrets of
// the watched namespaces.
func TestProxyWatchesOnlyItsBundle(t *testing.T) {
	bundle := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: "system", Name: "bundle"},
		Data:       bundleCertificates(gatewayObj("demo", "web"), newIndex(&Snapshot{})),
	}
	other := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "demo", Name: "secret"}}

	kube := kubefake.NewSimpleClientset(bundle, other)
	gw := gwfake.NewSimpleClientset()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	p := NewWithClients(Config{
		Mode:              ModeProxy,
		Debounce:          time.Millisecond,
		Gateways:          []string{"demo/web"},
		CertificateSecret: "system/bundle",
	}, nopApplier{}, kube, gw)

	go func() { _ = p.Run(ctx) }()

	select {
	case <-p.Ready():
	case <-time.After(5 * time.Second):
		t.Fatal("proxy not ready")
	}

	for _, a := range kube.Actions() {
		if a.GetResource().Resource != "secrets" {
			continue
		}

		if a.GetNamespace() != "system" {
			t.Fatalf("proxy lists secrets in namespace %q", a.GetNamespace())
		}

		if l, ok := a.(k8stesting.ListAction); ok && l.GetListRestrictions().Fields.String() != "metadata.name=bundle" {
			t.Fatalf("proxy lists secrets without name selector: %v", l.GetListRestrictions().Fields)
		}
	}
}

// Unchanged proxies must be compared against the cache, not the API.
func TestProvisionUnchangedUsesCache(t *testing.T) {
	t.Setenv("POD_NAMESPACE", "system")

	kube := kubefake.NewSimpleClientset()
	gw := gwfake.NewSimpleClientset()
	bg := context.Background()

	_, _ = gw.GatewayV1().GatewayClasses().Create(bg, class(), metav1.CreateOptions{})
	_, _ = gw.GatewayV1().Gateways("demo").Create(bg, gatewayObj("demo", "web", httpListener("http", 80, "")), metav1.CreateOptions{})

	ctx, cancel := context.WithCancel(bg)
	defer cancel()

	p := NewWithClients(Config{
		Mode:     ModeController,
		Debounce: time.Millisecond,
		Status:   Status{LeaderElection: LeaderElection{Disabled: true}},
		Proxy:    Proxy{Image: "turna:test", Replicas: 2},
	}, nopApplier{}, kube, gw)

	go func() { _ = p.Run(ctx) }()

	name := proxyName(nsName{"demo", "web"})
	eventually(t, "proxy and cache", func() bool {
		pl := p.proxies()
		if pl == nil {
			return false
		}

		_, errD := pl.deployments.Deployments("system").Get(name)
		_, errP := pl.pdbs.PodDisruptionBudgets("system").Get(name)
		_, errS := pl.secrets.Secrets("system").Get(name)

		return errD == nil && errP == nil && errS == nil
	})

	// wait for the event-driven syncs caused by the creations to settle
	time.Sleep(200 * time.Millisecond)
	kube.ClearActions()

	p.notify()
	time.Sleep(200 * time.Millisecond)

	for _, a := range kube.Actions() {
		switch a.GetVerb() {
		case "get", "list", "update", "create", "delete":
			t.Errorf("unchanged gateway made a %s %s request", a.GetVerb(), a.GetResource().Resource)
		}
	}
}

func TestExternalNameBackendIsOptIn(t *testing.T) {
	svc := &corev1.Service{
		ObjectMeta: objMeta("app", "metadata"),
		Spec: corev1.ServiceSpec{
			Type:         corev1.ServiceTypeExternalName,
			ExternalName: "169.254.169.254",
			Ports:        []corev1.ServicePort{{Name: "http", Port: 80}},
		},
	}

	snap := &Snapshot{
		GatewayClasses: []*gwv1.GatewayClass{class()},
		Gateways:       []*gwv1.Gateway{gatewayObj("app", "gw", httpListener("http", 80, ""))},
		HTTPRoutes:     []*gwv1.HTTPRoute{routeObj("app", "r", "gw", nil, backendRule("/", "metadata", 80))},
		Services:       []*corev1.Service{svc},
	}

	upstreams := func(res *Result) []string {
		var out []string
		for _, m := range res.Server.HTTP.Middlewares {
			if m.ServiceMiddleware != nil {
				for _, s := range m.ServiceMiddleware.LoadBalancer.Servers {
					out = append(out, s.URL)
				}
			}
		}

		return out
	}

	res := Translate(&Config{}, snap)
	if u := upstreams(res); len(u) != 0 {
		t.Fatalf("ExternalName backend used by default: %v", u)
	}

	c := condition(t, res.HTTPRouteStatus(nsName{"app", "r"})[0].Conditions, string(gwv1.RouteConditionResolvedRefs))
	if c.Status != metav1.ConditionFalse || c.Reason != string(gwv1.RouteReasonUnsupportedValue) {
		t.Fatalf("resolved refs = %+v", c)
	}

	res = Translate(&Config{Backend: Backend{AllowExternalName: true}}, snap)
	if u := upstreams(res); len(u) != 1 || u[0] != "http://169.254.169.254:80" {
		t.Fatalf("opted in ExternalName upstreams = %v", u)
	}
}

func TestListenerOnHealthPort(t *testing.T) {
	for _, cfg := range []Config{
		{Mode: ModeController, PortOffset: 8000},
		{Mode: ModeController, PortOffset: 8000, Proxy: Proxy{HealthPort: 9901}},
		{PortOffset: 8000, HealthAddress: ":9901"},
	} {
		t.Run(cfg.mode(), func(t *testing.T) {
			gw := gatewayObj("app", "gw", httpListener("ok", 80, ""), httpListener("health", 1901, "x.example.com"))
			res := Translate(&cfg, &Snapshot{GatewayClasses: []*gwv1.GatewayClass{class()}, Gateways: []*gwv1.Gateway{gw}})

			for _, l := range res.Gateways[key(gw)].Listeners {
				c := meta.FindStatusCondition(l.Conditions, string(gwv1.ListenerConditionAccepted))
				want := l.Name == "ok"
				if (c.Status == metav1.ConditionTrue) != want {
					t.Errorf("listener %s accepted = %s (%s)", l.Name, c.Status, c.Message)
				}
				if !want && c.Reason != string(gwv1.ListenerReasonPortUnavailable) {
					t.Errorf("listener %s reason = %s", l.Name, c.Reason)
				}
			}

			if _, ok := res.Server.EntryPoints["k8s-1901"]; ok {
				t.Error("listener on the health port got an entrypoint")
			}
		})
	}

	gw := gatewayObj("app", "gw", httpListener("ok", 80, ""), httpListener("health", 1901, ""))
	for _, lp := range listenerPorts(gw, 8000, 9901) {
		if lp.port == 1901 {
			t.Fatal("health port exposed on the proxy")
		}
	}
}

// Addresses of an unsupported type reject the Gateway, it gets no proxy.
func TestGatewayWithUnsupportedAddressIsNotProvisioned(t *testing.T) {
	gw := gatewayObj("app", "gw", httpListener("http", 80, ""))
	gw.Spec.Addresses = []gwv1.GatewaySpecAddress{{Type: ptr(gwv1.HostnameAddressType), Value: "gw.example.com"}}

	res := Translate(&Config{}, &Snapshot{GatewayClasses: []*gwv1.GatewayClass{class()}, Gateways: []*gwv1.Gateway{gw}})
	if len(res.Server.EntryPoints) != 0 {
		t.Fatalf("not accepted gateway serves traffic: %v", res.Server.EntryPoints)
	}

	ctx := context.Background()
	kube := kubefake.NewSimpleClientset()
	p := &Provider{kube: kube, cfg: Config{Proxy: Proxy{Namespace: "system", Image: "example:1"}}}

	if err := p.provision(ctx, []*gwv1.Gateway{gw}, nil, nil); err != nil {
		t.Fatal(err)
	}

	for _, a := range kube.Actions() {
		if a.GetVerb() == "create" {
			t.Fatalf("proxy resource created for a not accepted gateway: %s", a.GetResource().Resource)
		}
	}
}

func TestProxyImageDefaultsToControllerImage(t *testing.T) {
	t.Setenv("POD_NAMESPACE", "system")
	t.Setenv("POD_NAME", "turna-abc")

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: "system", Name: "turna-abc"},
		Spec: corev1.PodSpec{Containers: []corev1.Container{
			{Name: "sidecar", Image: "sidecar:1"},
			{Name: proxyContainer, Image: "ghcr.io/rakunlabs/turna:v1.2.3"},
		}},
	}

	p := NewWithClients(Config{Mode: ModeController}, nopApplier{}, kubefake.NewSimpleClientset(pod), gwfake.NewSimpleClientset())

	image, err := p.controllerImage(context.Background())
	if err != nil || image != "ghcr.io/rakunlabs/turna:v1.2.3" {
		t.Fatalf("controller image = %q, %v", image, err)
	}

	p.ownImage = image
	gw := gatewayObj("demo", "web", httpListener("http", 80, ""))
	dep := p.proxyDeployment("system", proxyName(key(gw)), proxyLabels(key(gw)), gw, nil, "hash")

	if c := dep.Spec.Template.Spec.Containers[0]; c.Image != image || c.ImagePullPolicy != corev1.PullIfNotPresent {
		t.Fatalf("proxy image = %s %s", c.Image, c.ImagePullPolicy)
	}

	// an explicit image wins
	p.cfg.Proxy.Image = "example:2"
	if got := p.proxyImage(); got != "example:2" {
		t.Fatalf("proxy image = %s", got)
	}

	// without an image the controller does not start
	t.Setenv("POD_NAME", "")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	bare := NewWithClients(Config{Mode: ModeController}, nopApplier{}, kubefake.NewSimpleClientset(), gwfake.NewSimpleClientset())
	if err := bare.Run(ctx); err == nil {
		t.Fatal("controller started without a proxy image")
	}
}

// Route kinds are rediscovered so CRDs installed later are watched, and a
// failing discovery is retried instead of assuming every kind exists.
func TestRouteKindsDiscovery(t *testing.T) {
	prev := discoveryInterval
	discoveryInterval = 20 * time.Millisecond
	t.Cleanup(func() { discoveryInterval = prev })

	gw := gwfake.NewSimpleClientset()

	// The fake discovery reads Resources without a lock, so they are set
	// only by the reactor, which runs under the fake's lock.
	var (
		calls   int
		install atomic.Bool
	)
	gw.PrependReactor("get", "resource", func(k8stesting.Action) (bool, runtime.Object, error) {
		calls++
		if calls == 1 {
			return true, nil, fmt.Errorf("temporary discovery error")
		}

		list := &metav1.APIResourceList{GroupVersion: gwv1.GroupVersion.String()}
		if install.Load() {
			list.APIResources = []metav1.APIResource{{Name: "tcproutes", Kind: kindTCPRoute}}
		}
		gw.Resources = []*metav1.APIResourceList{list}

		return false, nil, nil
	})

	kube := kubefake.NewSimpleClientset()
	// the fake stores access reviews by name, so a second review would fail
	kube.PrependReactor("create", "selfsubjectaccessreviews", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, &authorizationv1.SelfSubjectAccessReview{Status: authorizationv1.SubjectAccessReviewStatus{Allowed: true}}, nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	p := NewWithClients(Config{Debounce: time.Millisecond, Status: Status{Disabled: true}}, nopApplier{}, kube, gw)
	go func() { defer close(done); _ = p.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-done })

	select {
	case <-p.Ready():
	case <-time.After(5 * time.Second):
		t.Fatal("provider not ready after a failed discovery")
	}

	watched := func(resource string) bool {
		for _, a := range gw.Actions() {
			if a.GetVerb() == "list" && a.GetResource() == (schema.GroupVersionResource{Group: gwv1.GroupName, Version: "v1", Resource: resource}) {
				return true
			}
		}

		return false
	}

	if watched("tcproutes") {
		t.Fatal("uninstalled TCPRoute is watched")
	}

	install.Store(true)

	eventually(t, "TCPRoute watched after its CRD is installed", func() bool { return watched("tcproutes") })
}

// ReferenceGrant is watched as v1 when served, and as v1beta1 on clusters
// with older CRDs.
func TestReferenceGrantVersion(t *testing.T) {
	for _, served := range []bool{true, false} {
		t.Run(fmt.Sprintf("v1=%v", served), func(t *testing.T) {
			gw := gwfake.NewSimpleClientset()
			gw.PrependReactor("get", "resource", func(k8stesting.Action) (bool, runtime.Object, error) {
				list := &metav1.APIResourceList{GroupVersion: gwv1.GroupVersion.String()}
				if served {
					list.APIResources = []metav1.APIResource{{Name: "referencegrants", Kind: kindReferenceGrant}}
				}
				gw.Resources = []*metav1.APIResourceList{list}

				return false, nil, nil
			})

			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})

			p := NewWithClients(Config{Debounce: time.Millisecond, Status: Status{Disabled: true}}, nopApplier{}, kubefake.NewSimpleClientset(), gw)
			go func() { defer close(done); _ = p.Run(ctx) }()
			t.Cleanup(func() { cancel(); <-done })

			select {
			case <-p.Ready():
			case <-time.After(5 * time.Second):
				t.Fatal("provider not ready")
			}

			want := "v1beta1"
			if served {
				want = "v1"
			}

			var versions []string
			for _, a := range gw.Actions() {
				if a.GetVerb() == "list" && a.GetResource().Resource == "referencegrants" {
					versions = append(versions, a.GetResource().Version)
				}
			}

			if len(versions) == 0 || slices.ContainsFunc(versions, func(v string) bool { return v != want }) {
				t.Fatalf("referencegrants listed with versions %v, want %s", versions, want)
			}
		})
	}
}
