package kubernetes

import (
	"fmt"
	"maps"
	"net"
	"slices"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/rakunlabs/turna/pkg/server"
	shttp "github.com/rakunlabs/turna/pkg/server/http"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/gateway"
	"github.com/rakunlabs/turna/pkg/server/tcp"
	"github.com/rakunlabs/turna/pkg/server/udp"
)

const (
	groupGateway = gwv1.GroupName
	kindGateway  = "Gateway"
	kindRoute    = "HTTPRoute"
	kindService  = "Service"
	kindSecret   = "Secret"
)

// routeKey identifies a route of any kind.
type routeKey struct {
	Kind gwv1.Kind
	nsName
}

// Result is the translated configuration and the status to report.
type Result struct {
	Server *server.Server

	// served gateways of this controller
	served []*gwv1.Gateway

	GatewayClasses map[string][]metav1.Condition
	Gateways       map[nsName]*GatewayStatus
	Routes         map[routeKey][]gwv1.RouteParentStatus
	// BackendTLSPolicy status per policy
	Policies map[nsName][]gwv1.PolicyAncestorStatus

	// BackendTLSPolicies applied to the backends of each served gateway;
	// their CA certificates go into the proxy bundle of the gateway
	gatewayPolicies map[nsName]map[nsName]struct{}
}

// HTTPRouteStatus returns the parent status of an HTTPRoute.
func (r *Result) HTTPRouteStatus(k nsName) []gwv1.RouteParentStatus {
	return r.Routes[routeKey{Kind: kindRoute, nsName: k}]
}

type GatewayStatus struct {
	Generation int64
	Conditions []metav1.Condition
	Listeners  []gwv1.ListenerStatus
}

// listener is a listener of a served gateway.
type listener struct {
	gw      *gwv1.Gateway
	spec    gwv1.Listener
	status  *gwv1.ListenerStatus
	family  family
	valid   bool
	port    int
	entry   string
	secure  bool
	certs   []shttp.Certificate
	matches int

	// passthrough TLS listener
	passthrough bool

	// hostnames attached by HTTPRoutes and GRPCRoutes, to resolve
	// conflicts between both kinds
	httpHosts map[string]gwv1.Kind
}

type translator struct {
	cfg *Config
	idx *index
	now metav1.Time

	out *Result

	// http entrypoint -> host -> rules collected for that host
	hosts map[string]map[string]*hostRules
	// valid HTTP listeners of each entrypoint, for listener isolation
	entryListeners map[string][]*listener
	// entrypoint -> listener family
	families map[string]family
	// http entrypoints serving TLS
	secure map[string]bool

	// stream entrypoint -> routes in attachment order
	streams map[string][]*streamRoute

	middlewares map[string]shttp.HTTPMiddleware

	// backend TLS of Service ports, resolved from BackendTLSPolicies
	backendTLS map[policyTarget]backendTLS
	policies   map[nsName]*policyState
	// every policy targeting a Service section, including conflicted ones
	policyTargets map[policyTarget][]nsName
	// gateways of the route being translated
	routeGateways []nsName
}

type hostRules struct {
	entry string
	host  string
	rules []gateway.Rule
	grpc  []gateway.Rule
}

// streamRoute is a TLS, TCP or UDP route attached to a stream entrypoint.
type streamRoute struct {
	li       *listener
	hosts    []string
	backends []streamBackend
}

// Translate converts a snapshot to a server configuration.
func Translate(cfg *Config, s *Snapshot) *Result {
	t := &translator{
		cfg: cfg,
		idx: newIndex(s),
		now: metav1.NewTime(time.Now()),
		out: &Result{
			Server: &server.Server{
				EntryPoints: make(map[string]server.EntryPoint),
				HTTP: shttp.HTTP{
					Routers:     make(map[string]shttp.Router),
					Middlewares: make(map[string]shttp.HTTPMiddleware),
					TLS:         shttp.TLS{Store: make(map[string][]shttp.Certificate)},
				},
				TCP: tcp.TCP{
					Routers:     make(map[string]tcp.Router),
					Middlewares: make(map[string]tcp.TCPMiddleware),
				},
				UDP: udp.UDP{
					Routers:     make(map[string]udp.Router),
					Middlewares: make(map[string]udp.UDPMiddleware),
				},
			},
			GatewayClasses: make(map[string][]metav1.Condition),
			Gateways:       make(map[nsName]*GatewayStatus),
			Routes:         make(map[routeKey][]gwv1.RouteParentStatus),
			Policies:       make(map[nsName][]gwv1.PolicyAncestorStatus),

			gatewayPolicies: make(map[nsName]map[nsName]struct{}),
		},
		hosts:          make(map[string]map[string]*hostRules),
		entryListeners: make(map[string][]*listener),
		families:       make(map[string]family),
		secure:         make(map[string]bool),
		streams:        make(map[string][]*streamRoute),
		middlewares:    make(map[string]shttp.HTTPMiddleware),
		backendTLS:     make(map[policyTarget]backendTLS),
		policies:       make(map[nsName]*policyState),
		policyTargets:  make(map[policyTarget][]nsName),
	}

	classes := t.classes(s.GatewayClasses)
	t.backendTLSPolicies(s.BackendTLSPolicies)

	listeners := make(map[nsName][]*listener)

	for _, gw := range s.Gateways {
		if _, ok := classes[string(gw.Spec.GatewayClassName)]; !ok {
			continue
		}

		if !t.servesGateway(gw) {
			continue
		}

		t.out.served = append(t.out.served, gw)
		listeners[key(gw)] = t.gateway(gw)

		// unsupported gateway fields: the gateway is not accepted and must
		// not serve traffic
		if _, _, _, rejected := gatewayRejection(gw); rejected {
			for _, li := range listeners[key(gw)] {
				li.valid = false
			}
		}
	}

	if cfg.mode() == ModeController {
		// every gateway has its own proxy, so ports only conflict within a
		// gateway; the controller translates only for status
		for k, lis := range listeners {
			t.mergePorts(map[nsName][]*listener{k: lis})
		}
	} else {
		t.mergePorts(listeners)
	}

	for _, route := range t.routeInfos(s) {
		t.route(route, listeners)
	}

	t.finishGateways(s.Gateways, listeners)
	t.finishPolicies()
	t.build()
	t.buildStreams()

	return t.out
}

// gatewayRejection reports Gateway fields that are not supported and make the
// whole gateway not accepted, with the Accepted and Programmed reasons.
func gatewayRejection(gw *gwv1.Gateway) (accepted, programmed, msg string, rejected bool) {
	switch {
	case len(gw.Spec.Addresses) > 0:
		return string(gwv1.GatewayReasonUnsupportedAddress), string(gwv1.GatewayReasonAddressNotUsable), "spec.addresses is not supported", true
	case gw.Spec.Infrastructure != nil && gw.Spec.Infrastructure.ParametersRef != nil:
		return string(gwv1.GatewayReasonInvalidParameters), string(gwv1.GatewayReasonInvalid), "spec.infrastructure.parametersRef is not supported", true
	case gw.Spec.TLS != nil && gw.Spec.TLS.Backend != nil && gw.Spec.TLS.Backend.ClientCertificateRef != nil:
		return string(gwv1.GatewayReasonInvalidClientCertificateRef), string(gwv1.GatewayReasonInvalid), "spec.tls.backend.clientCertificateRef is not supported", true
	default:
		return "", "", "", false
	}
}

// requiresClientValidation reports whether the gateway asks for client
// certificate validation on an HTTPS listener port.
func requiresClientValidation(gw *gwv1.Gateway, port gwv1.PortNumber) bool {
	if gw.Spec.TLS == nil || gw.Spec.TLS.Frontend == nil {
		return false
	}

	f := gw.Spec.TLS.Frontend
	for _, pp := range f.PerPort {
		if pp.Port == port {
			return pp.TLS.Validation != nil
		}
	}

	return f.Default.Validation != nil
}

func (t *translator) servesGateway(gw *gwv1.Gateway) bool {
	if len(t.cfg.Gateways) == 0 {
		return true
	}

	return slices.Contains(t.cfg.Gateways, key(gw).String())
}

func (t *translator) classes(list []*gwv1.GatewayClass) map[string]*gwv1.GatewayClass {
	out := make(map[string]*gwv1.GatewayClass)

	for _, gc := range list {
		if string(gc.Spec.ControllerName) != t.cfg.controllerName() {
			continue
		}
		if t.cfg.GatewayClass != "" && gc.Name != t.cfg.GatewayClass {
			continue
		}

		out[gc.Name] = gc

		cond := []metav1.Condition{t.condition(gc.Generation, string(gwv1.GatewayClassConditionStatusAccepted), true, string(gwv1.GatewayClassReasonAccepted), "accepted by turna")}
		if gc.Spec.ParametersRef != nil {
			cond[0] = t.condition(gc.Generation, string(gwv1.GatewayClassConditionStatusAccepted), false, string(gwv1.GatewayClassReasonInvalidParameters), "parametersRef is not supported")
			delete(out, gc.Name)
		}

		t.out.GatewayClasses[gc.Name] = cond
	}

	return out
}

func (t *translator) condition(gen int64, typ string, ok bool, reason, msg string) metav1.Condition {
	status := metav1.ConditionFalse
	if ok {
		status = metav1.ConditionTrue
	}

	return metav1.Condition{
		Type:               typ,
		Status:             status,
		Reason:             reason,
		Message:            msg,
		ObservedGeneration: gen,
		LastTransitionTime: t.now,
	}
}

// supportedKinds returns the route kinds a listener accepts and whether all
// requested kinds are supported.
func supportedKinds(l gwv1.Listener) ([]gwv1.RouteGroupKind, bool) {
	group := gwv1.Group(groupGateway)
	possible := routeKinds(l.Protocol)

	if l.AllowedRoutes == nil || len(l.AllowedRoutes.Kinds) == 0 {
		out := make([]gwv1.RouteGroupKind, 0, len(possible))
		for _, k := range possible {
			out = append(out, gwv1.RouteGroupKind{Group: &group, Kind: k})
		}

		return out, true
	}

	out := make([]gwv1.RouteGroupKind, 0, len(l.AllowedRoutes.Kinds))
	valid := true

	for _, k := range l.AllowedRoutes.Kinds {
		if ptrOr(k.Group, gwv1.Group(groupGateway)) == groupGateway && slices.Contains(possible, k.Kind) {
			out = append(out, gwv1.RouteGroupKind{Group: &group, Kind: k.Kind})

			continue
		}

		valid = false
	}

	return out, valid
}

func (t *translator) gateway(gw *gwv1.Gateway) []*listener {
	out := make([]*listener, 0, len(gw.Spec.Listeners))
	gen := gw.Generation

	for _, l := range gw.Spec.Listeners {
		st := &gwv1.ListenerStatus{Name: l.Name}
		li := &listener{gw: gw, spec: l, status: st, port: int(l.Port), valid: true, family: protocolFamily(l.Protocol)}

		kinds, kindsOK := supportedKinds(l)
		st.SupportedKinds = kinds

		accepted := t.condition(gen, string(gwv1.ListenerConditionAccepted), true, string(gwv1.ListenerReasonAccepted), "")
		resolved := t.condition(gen, string(gwv1.ListenerConditionResolvedRefs), true, string(gwv1.ListenerReasonResolvedRefs), "")

		switch l.Protocol {
		case gwv1.HTTPProtocolType, gwv1.TCPProtocolType, gwv1.UDPProtocolType:
		case gwv1.HTTPSProtocolType:
			li.secure = true

			if l.TLS == nil || ptrOr(l.TLS.Mode, gwv1.TLSModeTerminate) != gwv1.TLSModeTerminate {
				accepted = t.condition(gen, string(gwv1.ListenerConditionAccepted), false, string(gwv1.ListenerReasonUnsupportedValue), "HTTPS listeners support tls mode Terminate only")
				li.valid = false

				break
			}

			if err := t.certificates(gw, l, li); err != nil {
				resolved = t.condition(gen, string(gwv1.ListenerConditionResolvedRefs), false, err.reason, err.msg)
				li.valid = false
			}

			// Client certificates are not validated; serving the listener
			// anyway would silently drop the required mutual TLS.
			if requiresClientValidation(gw, l.Port) {
				const msg = "client certificate validation (spec.tls.frontend) is not supported"
				accepted = t.condition(gen, string(gwv1.ListenerConditionAccepted), false, string(gwv1.ListenerReasonNoValidCACertificate), msg)
				resolved = t.condition(gen, string(gwv1.ListenerConditionResolvedRefs), false, string(gwv1.ListenerReasonInvalidCACertificateRef), msg)
				li.valid = false
			}
		case gwv1.TLSProtocolType:
			mode := gwv1.TLSModeTerminate
			if l.TLS != nil {
				mode = ptrOr(l.TLS.Mode, gwv1.TLSModeTerminate)
			}

			if mode == gwv1.TLSModePassthrough {
				li.passthrough = true

				break
			}

			if l.TLS == nil {
				resolved = t.condition(gen, string(gwv1.ListenerConditionResolvedRefs), false, string(gwv1.ListenerReasonInvalidCertificateRef), "tls is required")
				li.valid = false

				break
			}

			if err := t.certificates(gw, l, li); err != nil {
				resolved = t.condition(gen, string(gwv1.ListenerConditionResolvedRefs), false, err.reason, err.msg)
				li.valid = false
			}
		default:
			accepted = t.condition(gen, string(gwv1.ListenerConditionAccepted), false, string(gwv1.ListenerReasonUnsupportedProtocol), fmt.Sprintf("protocol %s is not supported", l.Protocol))
			li.valid = false
			st.SupportedKinds = []gwv1.RouteGroupKind{}
		}

		if !kindsOK {
			resolved = t.condition(gen, string(gwv1.ListenerConditionResolvedRefs), false, string(gwv1.ListenerReasonInvalidRouteKinds), "route kind is not supported by the listener protocol")
			if len(kinds) == 0 {
				li.valid = false
			}
		}

		if local := li.port + t.cfg.PortOffset; li.valid && (local < 1 || local > 65535) {
			accepted = t.condition(gen, string(gwv1.ListenerConditionAccepted), false, string(gwv1.ListenerReasonPortUnavailable),
				fmt.Sprintf("port %d with port offset %d is out of range", li.port, t.cfg.PortOffset))
			li.valid = false
		} else if li.valid && local == t.cfg.reservedPort() {
			accepted = t.condition(gen, string(gwv1.ListenerConditionAccepted), false, string(gwv1.ListenerReasonPortUnavailable),
				fmt.Sprintf("port %d with port offset %d is the health port", li.port, t.cfg.PortOffset))
			li.valid = false
		}

		st.Conditions = []metav1.Condition{accepted, resolved}
		out = append(out, li)
	}

	return out
}

type refError struct {
	reason string
	msg    string
}

func (t *translator) certificates(gw *gwv1.Gateway, l gwv1.Listener, li *listener) *refError {
	if len(l.TLS.CertificateRefs) == 0 {
		return &refError{string(gwv1.ListenerReasonInvalidCertificateRef), "certificateRefs is empty"}
	}

	for _, ref := range l.TLS.CertificateRefs {
		if ptrOr(ref.Group, "") != "" || ptrOr(ref.Kind, kindSecret) != kindSecret {
			return &refError{string(gwv1.ListenerReasonInvalidCertificateRef), fmt.Sprintf("unsupported certificate kind %s", ptrOr(ref.Kind, ""))}
		}

		ns := string(ptrOr(ref.Namespace, gwv1.Namespace(gw.Namespace)))
		if !t.idx.referenceAllowed(groupGateway, kindGateway, gw.Namespace, "", kindSecret, ns, string(ref.Name)) {
			return &refError{string(gwv1.ListenerReasonRefNotPermitted), fmt.Sprintf("secret %s/%s is not permitted by a ReferenceGrant", ns, ref.Name)}
		}

		secret, ok := t.idx.secrets[nsName{Namespace: ns, Name: string(ref.Name)}]
		if !ok {
			return &refError{string(gwv1.ListenerReasonInvalidCertificateRef), fmt.Sprintf("secret %s/%s not found", ns, ref.Name)}
		}

		cert, key := secret.Data[corev1.TLSCertKey], secret.Data[corev1.TLSPrivateKeyKey]
		if len(cert) == 0 || len(key) == 0 {
			return &refError{string(gwv1.ListenerReasonInvalidCertificateRef), fmt.Sprintf("secret %s/%s has no tls.crt/tls.key", ns, ref.Name)}
		}

		c := shttp.Certificate{Cert: string(cert), Key: string(key)}
		if err := validCertificate(c); err != nil {
			return &refError{string(gwv1.ListenerReasonInvalidCertificateRef), fmt.Sprintf("secret %s/%s: %v", ns, ref.Name, err)}
		}

		li.certs = append(li.certs, c)
	}

	return nil
}

// portKind tells which listeners can share a port: all HTTP, all HTTPS,
// TLS (passthrough and terminate mixed by SNI), TCP or UDP. TCP and UDP
// listeners may use the same port number as they are different sockets.
func portKind(li *listener) string {
	switch {
	case li.family == familyHTTP && li.secure:
		return "https"
	case li.family == familyHTTP:
		return "http"
	default:
		return string(li.family)
	}
}

func entryName(li *listener) string {
	if li.family == familyUDP {
		return "k8s-udp-" + strconv.Itoa(li.port)
	}

	return "k8s-" + strconv.Itoa(li.port)
}

// mergePorts maps listener ports to entrypoints and detects conflicts. All
// gateways served by this process share entrypoints, listeners of different
// gateways on the same port are merged.
func (t *translator) mergePorts(all map[nsName][]*listener) {
	type portInfo struct {
		kind      string
		entry     string
		listeners []*listener
	}

	ports := make(map[string]*portInfo)

	keys := make([]nsName, 0, len(all))
	for k := range all {
		keys = append(keys, k)
	}

	slices.SortFunc(keys, func(a, b nsName) int { return strings.Compare(a.String(), b.String()) })

	for _, k := range keys {
		for _, li := range all[k] {
			if !li.valid {
				continue
			}

			entry := entryName(li)

			p, ok := ports[entry]
			if !ok {
				p = &portInfo{kind: portKind(li), entry: entry}
				ports[entry] = p
			}

			if p.kind != portKind(li) {
				li.valid = false
				t.setConflict(li, string(gwv1.ListenerReasonProtocolConflict), "port is used with another protocol")

				continue
			}

			hostname := string(ptrOr(li.spec.Hostname, ""))
			conflict := false
			for _, other := range p.listeners {
				if string(ptrOr(other.spec.Hostname, "")) == hostname && (other.gw == li.gw || li.family != familyHTTP) {
					conflict = true

					break
				}
			}

			if conflict {
				li.valid = false
				t.setConflict(li, string(gwv1.ListenerReasonHostnameConflict), "another listener uses the same port and hostname")

				continue
			}

			p.listeners = append(p.listeners, li)
		}
	}

	// sorted so certificates are stored in a stable order, the first
	// certificate of a host wins
	entries := slices.Sorted(maps.Keys(ports))

	for _, entry := range entries {
		p := ports[entry]
		port := p.listeners[0].port
		addr := net.JoinHostPort(t.cfg.Address, strconv.Itoa(port+t.cfg.PortOffset))

		network := ""
		if p.kind == string(familyUDP) {
			network = "udp"
		}

		t.out.Server.EntryPoints[entry] = server.EntryPoint{Address: addr, Network: network}
		t.families[entry] = p.listeners[0].family
		t.secure[entry] = p.kind == "https"

		for _, li := range p.listeners {
			li.entry = entry

			if li.family != familyHTTP {
				continue
			}

			t.entryListeners[entry] = append(t.entryListeners[entry], li)

			for _, c := range li.certs {
				host := string(ptrOr(li.spec.Hostname, ""))
				if host == "" {
					host = "default"
				}

				t.out.Server.HTTP.TLS.Store[host] = append(t.out.Server.HTTP.TLS.Store[host], c)
			}
		}
	}
}

func (t *translator) setConflict(li *listener, reason, msg string) {
	gen := li.gw.Generation

	for i, c := range li.status.Conditions {
		if c.Type == string(gwv1.ListenerConditionAccepted) && reason == string(gwv1.ListenerReasonProtocolConflict) {
			li.status.Conditions[i] = t.condition(gen, string(gwv1.ListenerConditionAccepted), false, reason, msg)
		}
	}

	li.status.Conditions = append(li.status.Conditions,
		t.condition(gen, string(gwv1.ListenerConditionConflicted), true, reason, msg),
	)
}

// route attaches a route to the listeners referenced by its parentRefs.
func (t *translator) route(route *routeInfo, gateways map[nsName][]*listener) {
	rk := routeKey{Kind: route.kind, nsName: route.key()}

	// translate once for all parents
	var (
		data       any
		refErr     *refError
		translated bool
	)

	translate := func() (any, *refError) {
		if !translated {
			t.routeGateways = route.gateways(gateways)
			data, refErr = route.translate()
			t.routeGateways = nil
			translated = true
		}

		return data, refErr
	}

	for _, ref := range route.parentRefs {
		if ptrOr(ref.Group, gwv1.Group(groupGateway)) != groupGateway || ptrOr(ref.Kind, kindGateway) != kindGateway {
			continue
		}

		gk := nsName{Namespace: string(ptrOr(ref.Namespace, gwv1.Namespace(route.meta.Namespace))), Name: string(ref.Name)}

		lis, ok := gateways[gk]
		if !ok {
			// not a gateway of this controller
			continue
		}

		status := gwv1.RouteParentStatus{
			ParentRef:      ref,
			ControllerName: gwv1.GatewayController(t.cfg.controllerName()),
		}

		accepted, resolved := t.attach(route, ref, lis, translate)
		status.Conditions = []metav1.Condition{accepted, resolved}

		t.out.Routes[rk] = append(t.out.Routes[rk], status)
	}
}

func (t *translator) attach(route *routeInfo, ref gwv1.ParentReference, lis []*listener, translate func() (any, *refError)) (metav1.Condition, metav1.Condition) {
	gen := route.meta.Generation
	resolvedOK := t.condition(gen, string(gwv1.RouteConditionResolvedRefs), true, string(gwv1.RouteReasonResolvedRefs), "")

	var candidates []*listener

	for _, li := range lis {
		if ref.SectionName != nil && *ref.SectionName != li.spec.Name {
			continue
		}

		if ref.Port != nil && int(*ref.Port) != li.port {
			continue
		}

		candidates = append(candidates, li)
	}

	if len(candidates) == 0 {
		return t.condition(gen, string(gwv1.RouteConditionAccepted), false, string(gwv1.RouteReasonNoMatchingParent), "no matching listener"), resolvedOK
	}

	var allowed []*listener
	for _, li := range candidates {
		if t.routeAllowed(route, li) {
			allowed = append(allowed, li)
		}
	}

	if len(allowed) == 0 {
		return t.condition(gen, string(gwv1.RouteConditionAccepted), false, string(gwv1.RouteReasonNotAllowedByListeners), "route is not allowed by listeners"), resolvedOK
	}

	type attachment struct {
		li    *listener
		hosts []string
	}

	var attached []attachment

	for _, li := range allowed {
		if route.hostless {
			attached = append(attached, attachment{li: li})

			continue
		}

		hosts, ok := intersectHosts(ptrOr(li.spec.Hostname, ""), route.hostnames)
		if !ok {
			continue
		}

		attached = append(attached, attachment{li: li, hosts: hosts})
	}

	if len(attached) == 0 {
		return t.condition(gen, string(gwv1.RouteConditionAccepted), false, string(gwv1.RouteReasonNoMatchingListenerHostname), "no matching listener hostname"), resolvedOK
	}

	// HTTPRoutes and GRPCRoutes must not share hostnames on a listener, the
	// older route wins
	if route.kind == kindRoute || route.kind == kindGRPCRoute {
		kept := attached[:0]

		for _, a := range attached {
			if a.li.httpHosts == nil {
				a.li.httpHosts = make(map[string]gwv1.Kind)
			}

			conflict := false
			for _, h := range a.hosts {
				for existing, kind := range a.li.httpHosts {
					if kind != route.kind && (h == "" || existing == "" || hostMatches(h, existing) || hostMatches(existing, h)) {
						conflict = true
						break
					}
				}
				if conflict {
					break
				}
			}

			if conflict {
				continue
			}

			for _, h := range a.hosts {
				a.li.httpHosts[h] = route.kind
			}

			kept = append(kept, a)
		}

		if len(kept) == 0 {
			return t.condition(gen, string(gwv1.RouteConditionAccepted), false, string(gwv1.RouteReasonNotAllowedByListeners), "hostnames are used by another route kind"), resolvedOK
		}

		attached = kept
	}

	data, refErr := translate()

	resolved := resolvedOK
	if refErr != nil {
		resolved = t.condition(gen, string(gwv1.RouteConditionResolvedRefs), false, refErr.reason, refErr.msg)
	}

	for _, a := range attached {
		// routes count as attached even when the listener cannot be
		// programmed, e.g. because of a missing certificate
		a.li.matches++

		if !a.li.valid || a.li.entry == "" {
			continue
		}

		switch v := data.(type) {
		case []gateway.Rule:
			for _, h := range a.hosts {
				// requests for the host belong to a more specific listener
				if t.shadowed(a.li, h) {
					continue
				}

				t.addRules(a.li.entry, h, v, route.kind == kindGRPCRoute)
			}
		case []streamBackend:
			t.streams[a.li.entry] = append(t.streams[a.li.entry], &streamRoute{li: a.li, hosts: a.hosts, backends: v})
		}
	}

	return t.condition(gen, string(gwv1.RouteConditionAccepted), true, string(gwv1.RouteReasonAccepted), ""), resolved
}

func (t *translator) routeAllowed(route *routeInfo, li *listener) bool {
	if !slices.ContainsFunc(li.status.SupportedKinds, func(k gwv1.RouteGroupKind) bool { return k.Kind == route.kind }) {
		return false
	}

	from := gwv1.NamespacesFromSame
	var selector *metav1.LabelSelector

	if li.spec.AllowedRoutes != nil && li.spec.AllowedRoutes.Namespaces != nil {
		from = ptrOr(li.spec.AllowedRoutes.Namespaces.From, gwv1.NamespacesFromSame)
		selector = li.spec.AllowedRoutes.Namespaces.Selector
	}

	switch from {
	case gwv1.NamespacesFromAll:
		return true
	case gwv1.NamespacesFromSame:
		return route.meta.Namespace == li.gw.Namespace
	case gwv1.NamespacesFromSelector:
		if selector == nil {
			return false
		}

		sel, err := metav1.LabelSelectorAsSelector(selector)
		if err != nil {
			return false
		}

		return sel.Matches(labels.Set(t.idx.namespaceLabels(route.meta.Namespace)))
	default:
		return false
	}
}

// intersectHosts returns the host rules to use for a route on a listener.
// An empty string means any host.
func intersectHosts(listenerHost gwv1.Hostname, routeHosts []gwv1.Hostname) ([]string, bool) {
	lh := string(listenerHost)

	if len(routeHosts) == 0 {
		return []string{lh}, true
	}

	var out []string

	for _, rh := range routeHosts {
		r := string(rh)

		switch {
		case lh == "":
			out = append(out, r)
		case hostMatches(lh, r):
			out = append(out, r)
		case hostMatches(r, lh):
			out = append(out, lh)
		}
	}

	slices.Sort(out)

	return slices.Compact(out), len(out) > 0
}

// hostMatches reports whether pattern matches host, both may be wildcards.
func hostMatches(pattern, host string) bool {
	if pattern == host {
		return true
	}

	if !strings.HasPrefix(pattern, "*.") {
		return false
	}

	suffix := pattern[1:]
	h := strings.TrimPrefix(host, "*")

	return strings.HasSuffix(h, suffix) && len(h) > len(suffix)
}

// shadowed reports whether requests for host are served by another listener
// on the same port with a more specific hostname than li. Listeners are
// isolated: a request is handled only by the routes of the most specific
// listener matching its host.
func (t *translator) shadowed(li *listener, host string) bool {
	own := string(ptrOr(li.spec.Hostname, ""))

	for _, other := range t.entryListeners[li.entry] {
		if other == li || !other.valid || other.family != familyHTTP {
			continue
		}

		oh := string(ptrOr(other.spec.Hostname, ""))
		if oh == "" || oh == own {
			continue
		}

		moreSpecific := own == "" || hostMatches(own, oh)
		if moreSpecific && (oh == host || hostMatches(oh, host)) {
			return true
		}
	}

	return false
}

// listenerHosts returns the sorted hostnames of the listeners of an
// entrypoint, nil with a single listener as no request can be misdirected.
func (t *translator) listenerHosts(entry string) []string {
	if len(t.entryListeners[entry]) < 2 {
		return nil
	}

	out := make([]string, 0, len(t.entryListeners[entry]))
	for _, li := range t.entryListeners[entry] {
		out = append(out, string(ptrOr(li.spec.Hostname, "")))
	}

	slices.Sort(out)

	return slices.Compact(out)
}

// isolatedHosts returns the listener hostnames of an entrypoint that another,
// less specific listener on the same entrypoint would also match.
func (t *translator) isolatedHosts(entry string) []string {
	var out []string

	for _, li := range t.entryListeners[entry] {
		h := string(ptrOr(li.spec.Hostname, ""))
		if h == "" {
			continue
		}

		for _, other := range t.entryListeners[entry] {
			oh := string(ptrOr(other.spec.Hostname, ""))
			if other != li && oh != h && (oh == "" || hostMatches(oh, h)) {
				out = append(out, h)

				break
			}
		}
	}

	return out
}

func (t *translator) addRules(entry, host string, rules []gateway.Rule, grpc bool) {
	byHost, ok := t.hosts[entry]
	if !ok {
		byHost = make(map[string]*hostRules)
		t.hosts[entry] = byHost
	}

	hr, ok := byHost[host]
	if !ok {
		hr = &hostRules{entry: entry, host: host}
		byHost[host] = hr
	}

	if grpc {
		hr.grpc = append(hr.grpc, rules...)

		return
	}

	hr.rules = append(hr.rules, rules...)
}

func (t *translator) finishGateways(list []*gwv1.Gateway, all map[nsName][]*listener) {
	for _, gw := range list {
		lis, ok := all[key(gw)]
		if !ok {
			continue
		}

		st := &GatewayStatus{Generation: gw.Generation}

		validCount := 0
		for _, li := range lis {
			programmed := t.condition(gw.Generation, string(gwv1.ListenerConditionProgrammed), li.valid, string(gwv1.ListenerReasonProgrammed), "")
			if !li.valid {
				programmed.Reason = string(gwv1.ListenerReasonInvalid)
			} else {
				validCount++
			}

			li.status.Conditions = append(li.status.Conditions, programmed)
			li.status.AttachedRoutes = int32(li.matches) //nolint:gosec // bounded by routes

			if !slices.ContainsFunc(li.status.Conditions, func(c metav1.Condition) bool {
				return c.Type == string(gwv1.ListenerConditionConflicted)
			}) {
				li.status.Conditions = append(li.status.Conditions,
					t.condition(gw.Generation, string(gwv1.ListenerConditionConflicted), false, string(gwv1.ListenerReasonNoConflicts), ""))
			}

			st.Listeners = append(st.Listeners, *li.status)
		}

		acceptedReason := string(gwv1.GatewayReasonAccepted)
		accepted := validCount > 0 || len(lis) == 0
		if validCount < len(lis) {
			acceptedReason = string(gwv1.GatewayReasonListenersNotValid)
		}

		if acceptReason, programReason, msg, rejected := gatewayRejection(gw); rejected {
			st.Conditions = []metav1.Condition{
				t.condition(gw.Generation, string(gwv1.GatewayConditionAccepted), false, acceptReason, msg),
				t.condition(gw.Generation, string(gwv1.GatewayConditionProgrammed), false, programReason, msg),
			}
		} else {
			st.Conditions = []metav1.Condition{
				t.condition(gw.Generation, string(gwv1.GatewayConditionAccepted), accepted, acceptedReason, ""),
				t.condition(gw.Generation, string(gwv1.GatewayConditionProgrammed), accepted, string(gwv1.GatewayReasonProgrammed), ""),
			}

			if !accepted {
				st.Conditions[1].Reason = string(gwv1.GatewayReasonInvalid)
			}
		}

		t.out.Gateways[key(gw)] = st
	}
}

// build creates one router and gateway middleware per http entrypoint and
// host.
func (t *translator) build() {
	srv := t.out.Server

	for entry := range srv.EntryPoints {
		if t.families[entry] != familyHTTP {
			continue
		}

		byHost := t.hosts[entry]
		if len(byHost) == 0 {
			// listeners without routes answer 404
			byHost = map[string]*hostRules{"": {entry: entry}}
		}

		// a listener hostname without routes still takes its requests, so
		// they answer 404 instead of reaching a less specific listener
		for _, h := range t.isolatedHosts(entry) {
			if _, ok := byHost[h]; !ok {
				byHost[h] = &hostRules{entry: entry, host: h}
			}
		}

		// unknown hosts reach a gateway too, which detects misdirected
		// requests before answering 404
		if t.secure[entry] && len(t.listenerHosts(entry)) > 0 {
			if _, ok := byHost[""]; !ok {
				byHost[""] = &hostRules{entry: entry}
			}
		}

		for host, hr := range byHost {
			name := sanitize("k8s", entry, host)

			g := &gateway.Gateway{Rules: hr.rules, ListenerPort: entryPort(entry)}
			if len(hr.grpc) > 0 {
				g.GRPC = hr.grpc
			}

			if t.secure[entry] {
				g.Listeners = t.listenerHosts(entry)
			}

			srv.HTTP.Middlewares[name] = shttp.HTTPMiddleware{Gateway: g}

			router := shttp.Router{
				Host:        host,
				Path:        []string{"/*"},
				Middlewares: []string{name},
				EntryPoints: []string{entry},
			}

			if t.secure[entry] {
				router.TLS = &struct{}{}
			}

			srv.HTTP.Routers[name] = router
		}
	}

	for name, m := range t.middlewares {
		srv.HTTP.Middlewares[name] = m
	}
}

func entryPort(entry string) int {
	p, _ := strconv.Atoi(strings.TrimPrefix(strings.TrimPrefix(entry, "k8s-udp-"), "k8s-"))

	return p
}

func readyEndpoint(ep discoveryv1.Endpoint) bool {
	return ep.Conditions.Ready == nil || *ep.Conditions.Ready
}
