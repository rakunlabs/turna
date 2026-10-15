package kubernetes

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func listenerSetObj(ns, name, gw string, created time.Time, listeners ...gwv1.Listener) *gwv1.ListenerSet {
	ls := &gwv1.ListenerSet{
		ObjectMeta: objMeta(ns, name),
		Spec: gwv1.ListenerSetSpec{
			ParentRef: gwv1.ParentGatewayReference{Name: gwv1.ObjectName(gw), Namespace: ptr(gwv1.Namespace("app"))},
		},
	}
	ls.CreationTimestamp = metav1.NewTime(created)

	for _, l := range listeners {
		ls.Spec.Listeners = append(ls.Spec.Listeners, gwv1.ListenerEntry(l))
	}

	return ls
}

func allowListeners(gw *gwv1.Gateway, from gwv1.FromNamespaces) *gwv1.Gateway {
	gw.Spec.AllowedListeners = &gwv1.AllowedListeners{Namespaces: &gwv1.ListenerNamespaces{From: ptr(from)}}

	return gw
}

func listenerSetParentRef(ns, name string, section string) gwv1.ParentReference {
	ref := gwv1.ParentReference{Group: ptr(gwv1.Group(groupGateway)), Kind: ptr(gwv1.Kind(kindListenerSet)), Name: gwv1.ObjectName(name), Namespace: ptr(gwv1.Namespace(ns))}
	if section != "" {
		ref.SectionName = ptr(gwv1.SectionName(section))
	}

	return ref
}

func entryStatus(t *testing.T, st *ListenerSetStatus, name string) gwv1.ListenerEntryStatus {
	t.Helper()

	for _, l := range st.Listeners {
		if string(l.Name) == name {
			return l
		}
	}

	t.Fatalf("listener %s not in status %+v", name, st.Listeners)

	return gwv1.ListenerEntryStatus{}
}

func TestListenerSetAllowed(t *testing.T) {
	now := time.Now()

	for name, tt := range map[string]struct {
		gw       *gwv1.Gateway
		ns       string
		accepted bool
		reason   gwv1.ListenerSetConditionReason
		attached *int32
	}{
		"default":    {gatewayObj("app", "gw", httpListener("http", 80, "gw.example.com")), "app", false, gwv1.ListenerSetReasonNotAllowed, nil},
		"none":       {allowListeners(gatewayObj("app", "gw", httpListener("http", 80, "gw.example.com")), gwv1.NamespacesFromNone), "app", false, gwv1.ListenerSetReasonNotAllowed, new(int32(0))},
		"same":       {allowListeners(gatewayObj("app", "gw", httpListener("http", 80, "gw.example.com")), gwv1.NamespacesFromSame), "app", true, gwv1.ListenerSetReasonAccepted, new(int32(1))},
		"same other": {allowListeners(gatewayObj("app", "gw", httpListener("http", 80, "gw.example.com")), gwv1.NamespacesFromSame), "other", false, gwv1.ListenerSetReasonNotAllowed, new(int32(0))},
		"all":        {allowListeners(gatewayObj("app", "gw", httpListener("http", 80, "gw.example.com")), gwv1.NamespacesFromAll), "other", true, gwv1.ListenerSetReasonAccepted, new(int32(1))},
	} {
		t.Run(name, func(t *testing.T) {
			ls := listenerSetObj(tt.ns, "ls", "gw", now, httpListener("ls", 80, "ls.example.com"))

			res := Translate(&Config{}, &Snapshot{GatewayClasses: []*gwv1.GatewayClass{class()}, Gateways: []*gwv1.Gateway{tt.gw}, ListenerSets: []*gwv1.ListenerSet{ls}})

			st := res.ListenerSets[key(ls)]
			if st == nil {
				t.Fatal("no listenerset status")
			}

			c := condition(t, st.Conditions, string(gwv1.ListenerSetConditionAccepted))
			if (c.Status == metav1.ConditionTrue) != tt.accepted || c.Reason != string(tt.reason) {
				t.Fatalf("accepted = %+v", c)
			}

			gst := res.Gateways[key(tt.gw)]
			if (gst.AttachedListenerSets == nil) != (tt.attached == nil) || (tt.attached != nil && *gst.AttachedListenerSets != *tt.attached) {
				t.Fatalf("attached = %v, want %v", gst.AttachedListenerSets, tt.attached)
			}

			// listeners of a ListenerSet are not gateway listeners
			if len(gst.Listeners) != 1 {
				t.Fatalf("gateway listeners = %+v", gst.Listeners)
			}

			_, served := res.Server.HTTP.Routers[sanitize("k8s", "k8s-80", "ls.example.com")]
			if served {
				t.Fatal("route-less listenerset host has a router")
			}
		})
	}
}

// Routes attach to the listeners of their parent only: a Gateway parent
// does not reach ListenerSet listeners and the other way round.
func TestListenerSetRouting(t *testing.T) {
	now := time.Now()
	gw := allowListeners(gatewayObj("app", "gw", httpListener("gw", 80, "gw.example.com")), gwv1.NamespacesFromAll)
	ls := listenerSetObj("app", "ls", "gw", now, httpListener("ls", 80, "ls.example.com"), httpListener("ls-tls", 8443, "tls.example.com"))
	svc, slice := serviceObj("app", "web", 8080)

	viaGateway := routeObj("app", "gw-route", "gw", nil, backendRule("/", "web", 8080))
	viaSet := routeObj("app", "ls-route", "", nil, backendRule("/", "web", 8080))
	viaSet.Spec.ParentRefs = []gwv1.ParentReference{listenerSetParentRef("app", "ls", "ls")}
	wrongSection := routeObj("app", "wrong", "gw", nil, backendRule("/", "web", 8080))
	wrongSection.Spec.ParentRefs[0].SectionName = ptr(gwv1.SectionName("ls"))

	res := Translate(&Config{}, &Snapshot{
		GatewayClasses: []*gwv1.GatewayClass{class()},
		Gateways:       []*gwv1.Gateway{gw},
		ListenerSets:   []*gwv1.ListenerSet{ls},
		HTTPRoutes:     []*gwv1.HTTPRoute{viaGateway, viaSet, wrongSection},
		Services:       []*corev1.Service{svc},
		EndpointSlices: []*discoveryv1.EndpointSlice{slice},
	})

	if _, ok := res.Server.EntryPoints["k8s-8443"]; !ok {
		t.Fatalf("listenerset port not served: %v", res.Server.EntryPoints)
	}

	for _, host := range []string{"gw.example.com", "ls.example.com"} {
		if _, ok := res.Server.HTTP.Routers[sanitize("k8s", "k8s-80", host)]; !ok {
			t.Errorf("no router for %s", host)
		}
	}

	if c := condition(t, res.HTTPRouteStatus(key(viaSet))[0].Conditions, string(gwv1.RouteConditionAccepted)); c.Status != metav1.ConditionTrue {
		t.Fatalf("listenerset route = %+v", c)
	}

	if c := condition(t, res.HTTPRouteStatus(key(wrongSection))[0].Conditions, string(gwv1.RouteConditionAccepted)); c.Reason != string(gwv1.RouteReasonNoMatchingParent) {
		t.Fatalf("gateway route with listenerset section = %+v", c)
	}

	st := res.ListenerSets[key(ls)]
	if l := entryStatus(t, st, "ls"); l.AttachedRoutes != 1 {
		t.Fatalf("attached routes = %d", l.AttachedRoutes)
	}

	if l := entryStatus(t, st, "ls-tls"); l.AttachedRoutes != 0 {
		t.Fatalf("attached routes of the other listener = %d", l.AttachedRoutes)
	}

	if l := res.Gateways[key(gw)].Listeners[0]; l.AttachedRoutes != 1 {
		t.Fatalf("gateway listener attached routes = %d", l.AttachedRoutes)
	}

	if res.ListenerSets[key(ls)].Generation != ls.Generation {
		t.Fatal("generation")
	}

	// the proxy exposes ListenerSet ports
	ports := listenerPorts(gw, 0, 0, attachedListenerSets(gw, newIndex(&Snapshot{ListenerSets: []*gwv1.ListenerSet{ls}}))...)
	if len(ports) != 2 || ports[1].port != 8443 {
		t.Fatalf("ports = %+v", ports)
	}
}

// Gateway listeners win conflicts, then older ListenerSets; a ListenerSet
// without valid listeners is not accepted.
func TestListenerSetConflicts(t *testing.T) {
	now := time.Now()
	gw := allowListeners(gatewayObj("app", "gw", httpListener("gw", 80, "gw.example.com")), gwv1.NamespacesFromSame)

	older := listenerSetObj("app", "b-older", "gw", now, httpListener("own", 80, "older.example.com"), httpListener("dup", 80, "gw.example.com"), httpListener("shared", 80, "shared.example.com"))
	newer := listenerSetObj("app", "a-newer", "gw", now.Add(time.Minute), httpListener("shared", 80, "shared.example.com"))
	tcp := listenerSetObj("app", "c-tcp", "gw", now.Add(2*time.Minute), gwv1.Listener{Name: "tcp", Port: 80, Protocol: gwv1.TCPProtocolType})

	res := Translate(&Config{}, &Snapshot{
		GatewayClasses: []*gwv1.GatewayClass{class()},
		Gateways:       []*gwv1.Gateway{gw},
		ListenerSets:   []*gwv1.ListenerSet{newer, older, tcp},
	})

	check := func(ls *gwv1.ListenerSet, listener string, accepted bool, reason gwv1.ListenerConditionReason) {
		t.Helper()

		l := entryStatus(t, res.ListenerSets[key(ls)], listener)
		a := condition(t, l.Conditions, string(gwv1.ListenerConditionAccepted))
		p := condition(t, l.Conditions, string(gwv1.ListenerConditionProgrammed))

		if (a.Status == metav1.ConditionTrue) != accepted || (p.Status == metav1.ConditionTrue) != accepted {
			t.Errorf("%s/%s accepted = %+v programmed = %+v", ls.Name, listener, a, p)
		}

		if !accepted {
			c := condition(t, l.Conditions, string(gwv1.ListenerConditionConflicted))
			if c.Status != metav1.ConditionTrue || c.Reason != string(reason) || a.Reason != string(reason) || p.Reason != string(reason) {
				t.Errorf("%s/%s conflicted = %+v", ls.Name, listener, l.Conditions)
			}
		}
	}

	check(older, "own", true, "")
	check(older, "dup", false, gwv1.ListenerReasonHostnameConflict)
	check(older, "shared", true, "")
	check(newer, "shared", false, gwv1.ListenerReasonHostnameConflict)
	check(tcp, "tcp", false, gwv1.ListenerReasonProtocolConflict)

	for ls, accepted := range map[*gwv1.ListenerSet]bool{older: true, newer: false, tcp: false} {
		c := condition(t, res.ListenerSets[key(ls)].Conditions, string(gwv1.ListenerSetConditionAccepted))
		if (c.Status == metav1.ConditionTrue) != accepted {
			t.Errorf("%s accepted = %+v", ls.Name, c)
		}

		if !accepted && c.Reason != string(gwv1.ListenerSetReasonListenersNotValid) {
			t.Errorf("%s reason = %s", ls.Name, c.Reason)
		}
	}

	if n := res.Gateways[key(gw)].AttachedListenerSets; n == nil || *n != 1 {
		t.Fatalf("attached = %v", n)
	}

	if !meta.IsStatusConditionTrue(res.Gateways[key(gw)].Conditions, string(gwv1.GatewayConditionAccepted)) {
		t.Fatal("gateway not accepted")
	}
}

// Routes and certificates are allowed relative to the ListenerSet namespace.
func TestListenerSetNamespaces(t *testing.T) {
	now := time.Now()
	gw := allowListeners(gatewayObj("app", "gw", httpListener("gw", 80, "gw.example.com")), gwv1.NamespacesFromAll)

	same := httpListener("same", 80, "same.example.com")
	same.AllowedRoutes = &gwv1.AllowedRoutes{Namespaces: &gwv1.RouteNamespaces{From: ptr(gwv1.NamespacesFromSame)}}
	ls := listenerSetObj("team", "ls", "gw", now, same, httpsListener("tls", "tls.example.com", gwv1.SecretObjectReference{Name: "cert", Namespace: ptr(gwv1.Namespace("certs"))}))

	inTeam := routeObj("team", "r", "", nil)
	inTeam.Spec.ParentRefs = []gwv1.ParentReference{listenerSetParentRef("team", "ls", "same")}
	inApp := routeObj("app", "r", "", nil)
	inApp.Spec.ParentRefs = []gwv1.ParentReference{listenerSetParentRef("team", "ls", "same")}

	cert := tlsSecret(t, "certs", "cert", "tls.example.com")
	gatewayGrant := &gwv1.ReferenceGrant{
		ObjectMeta: objMeta("certs", "gateway"),
		Spec: gwv1.ReferenceGrantSpec{
			From: []gwv1.ReferenceGrantFrom{{Group: groupGateway, Kind: kindGateway, Namespace: "app"}},
			To:   []gwv1.ReferenceGrantTo{{Kind: kindSecret}},
		},
	}
	setGrant := gatewayGrant.DeepCopy()
	setGrant.Name = "set"
	setGrant.Spec.From = []gwv1.ReferenceGrantFrom{{Group: groupGateway, Kind: kindListenerSet, Namespace: "team"}}

	snap := func(grants ...*gwv1.ReferenceGrant) *Snapshot {
		return &Snapshot{
			GatewayClasses:  []*gwv1.GatewayClass{class()},
			Gateways:        []*gwv1.Gateway{gw},
			ListenerSets:    []*gwv1.ListenerSet{ls},
			HTTPRoutes:      []*gwv1.HTTPRoute{inTeam, inApp},
			Secrets:         []*corev1.Secret{cert},
			ReferenceGrants: grants,
		}
	}

	// a grant for the gateway does not cover its ListenerSets
	res := Translate(&Config{}, snap(gatewayGrant))
	if c := condition(t, entryStatus(t, res.ListenerSets[key(ls)], "tls").Conditions, string(gwv1.ListenerConditionResolvedRefs)); c.Reason != string(gwv1.ListenerReasonRefNotPermitted) {
		t.Fatalf("resolved refs = %+v", c)
	}

	if len(bundleCertificates(gw, newIndex(snap(gatewayGrant)))) != 0 {
		t.Fatal("certificate bundled without a ListenerSet grant")
	}

	res = Translate(&Config{}, snap(setGrant))
	if c := condition(t, entryStatus(t, res.ListenerSets[key(ls)], "tls").Conditions, string(gwv1.ListenerConditionResolvedRefs)); c.Status != metav1.ConditionTrue {
		t.Fatalf("resolved refs with grant = %+v", c)
	}

	if len(bundleCertificates(gw, newIndex(snap(setGrant)))) != 2 {
		t.Fatal("ListenerSet certificate is not bundled")
	}

	if c := condition(t, res.HTTPRouteStatus(key(inTeam))[0].Conditions, string(gwv1.RouteConditionAccepted)); c.Status != metav1.ConditionTrue {
		t.Fatalf("route in the listenerset namespace = %+v", c)
	}

	if c := condition(t, res.HTTPRouteStatus(key(inApp))[0].Conditions, string(gwv1.RouteConditionAccepted)); c.Reason != string(gwv1.RouteReasonNotAllowedByListeners) {
		t.Fatalf("route in the gateway namespace = %+v", c)
	}
}

// A gateway that is not accepted serves no ListenerSet listeners.
func TestListenerSetParentNotAccepted(t *testing.T) {
	gw := allowListeners(gatewayObj("app", "gw", gwv1.Listener{Name: "bad", Port: 80, Protocol: "FOO"}), gwv1.NamespacesFromSame)
	ls := listenerSetObj("app", "ls", "gw", time.Now(), httpListener("ls", 81, "ls.example.com"))

	res := Translate(&Config{}, &Snapshot{GatewayClasses: []*gwv1.GatewayClass{class()}, Gateways: []*gwv1.Gateway{gw}, ListenerSets: []*gwv1.ListenerSet{ls}})

	if len(res.Server.EntryPoints) != 0 {
		t.Fatalf("entrypoints = %v", res.Server.EntryPoints)
	}

	c := condition(t, res.ListenerSets[key(ls)].Conditions, string(gwv1.ListenerSetConditionAccepted))
	if c.Status != metav1.ConditionFalse || c.Reason != string(gwv1.ListenerSetReasonParentNotAccepted) {
		t.Fatalf("accepted = %+v", c)
	}

	if n := res.Gateways[key(gw)].AttachedListenerSets; n == nil || *n != 0 {
		t.Fatalf("attached = %v", n)
	}
}

func TestListenerSetPendingWithParent(t *testing.T) {
	st := &ListenerSetStatus{Conditions: []metav1.Condition{
		{Type: string(gwv1.ListenerSetConditionAccepted), Status: metav1.ConditionTrue},
		{Type: string(gwv1.ListenerSetConditionProgrammed), Status: metav1.ConditionTrue, Reason: string(gwv1.ListenerSetReasonProgrammed)},
	}}

	out := listenerSetPending(st, "waiting")
	if c := condition(t, out.Conditions, string(gwv1.ListenerSetConditionProgrammed)); c.Status != metav1.ConditionFalse || c.Reason != string(gwv1.ListenerSetReasonPending) {
		t.Fatalf("programmed = %+v", c)
	}

	if st.Conditions[1].Status != metav1.ConditionTrue {
		t.Fatal("input modified")
	}
}
