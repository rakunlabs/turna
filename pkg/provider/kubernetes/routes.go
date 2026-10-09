package kubernetes

import (
	"cmp"
	"slices"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
)

const (
	kindGRPCRoute = "GRPCRoute"
	kindTLSRoute  = "TLSRoute"
	kindTCPRoute  = "TCPRoute"
	kindUDPRoute  = "UDPRoute"
)

// family groups listener protocols sharing a port: http listeners (HTTP and
// HTTPS) are served by the HTTP server, tls and tcp listeners by the TCP
// server and udp listeners by the UDP server.
type family string

const (
	familyHTTP family = "http"
	familyTLS  family = "tls"
	familyTCP  family = "tcp"
	familyUDP  family = "udp"
)

// routeKinds lists route kinds a listener protocol can attach.
func routeKinds(p gwv1.ProtocolType) []gwv1.Kind {
	switch p {
	case gwv1.HTTPProtocolType, gwv1.HTTPSProtocolType:
		return []gwv1.Kind{kindRoute, kindGRPCRoute}
	case gwv1.TLSProtocolType:
		return []gwv1.Kind{kindTLSRoute}
	case gwv1.TCPProtocolType:
		return []gwv1.Kind{kindTCPRoute}
	case gwv1.UDPProtocolType:
		return []gwv1.Kind{kindUDPRoute}
	default:
		return nil
	}
}

func protocolFamily(p gwv1.ProtocolType) family {
	switch p {
	case gwv1.HTTPProtocolType, gwv1.HTTPSProtocolType:
		return familyHTTP
	case gwv1.TLSProtocolType:
		return familyTLS
	case gwv1.TCPProtocolType:
		return familyTCP
	case gwv1.UDPProtocolType:
		return familyUDP
	default:
		return ""
	}
}

// routeInfo is the kind independent view of a route used for attachment.
type routeInfo struct {
	kind       gwv1.Kind
	meta       metav1.ObjectMeta
	parentRefs []gwv1.ParentReference
	hostnames  []gwv1.Hostname
	// hostless kinds (TCP, UDP) ignore hostnames
	hostless bool

	// translate returns the route data for its attached listeners
	translate func() (any, *refError)
}

func (r *routeInfo) key() nsName {
	return nsName{Namespace: r.meta.Namespace, Name: r.meta.Name}
}

func sortRouteInfos(routes []*routeInfo) {
	slices.SortFunc(routes, func(a, b *routeInfo) int {
		if c := a.meta.CreationTimestamp.Compare(b.meta.CreationTimestamp.Time); c != 0 {
			return c
		}

		return cmp.Compare(a.key().String(), b.key().String())
	})
}

func (t *translator) routeInfos(s *Snapshot) []*routeInfo {
	var out []*routeInfo

	for _, r := range s.HTTPRoutes {
		out = append(out, &routeInfo{
			kind: kindRoute, meta: r.ObjectMeta, parentRefs: r.Spec.ParentRefs, hostnames: r.Spec.Hostnames,
			translate: func() (any, *refError) { return t.rules(r) },
		})
	}

	for _, r := range s.GRPCRoutes {
		out = append(out, &routeInfo{
			kind: kindGRPCRoute, meta: r.ObjectMeta, parentRefs: r.Spec.ParentRefs, hostnames: r.Spec.Hostnames,
			translate: func() (any, *refError) { return t.grpcRules(r) },
		})
	}

	for _, r := range s.TLSRoutes {
		out = append(out, &routeInfo{
			kind: kindTLSRoute, meta: r.ObjectMeta, parentRefs: r.Spec.ParentRefs, hostnames: r.Spec.Hostnames,
			translate: func() (any, *refError) {
				return t.streamBackends(kindTLSRoute, r.Namespace, firstRuleBackends(r.Spec.Rules, func(x gwv1.TLSRouteRule) []gwv1.BackendRef { return x.BackendRefs }), corev1.ProtocolTCP)
			},
		})
	}

	for _, r := range s.TCPRoutes {
		out = append(out, &routeInfo{
			kind: kindTCPRoute, meta: r.ObjectMeta, parentRefs: r.Spec.ParentRefs, hostless: true,
			translate: func() (any, *refError) {
				return t.streamBackends(kindTCPRoute, r.Namespace, firstRuleBackends(r.Spec.Rules, func(x gwv1.TCPRouteRule) []gwv1.BackendRef { return x.BackendRefs }), corev1.ProtocolTCP)
			},
		})
	}

	for _, r := range s.UDPRoutes {
		out = append(out, &routeInfo{
			kind: kindUDPRoute, meta: r.ObjectMeta, parentRefs: r.Spec.ParentRefs, hostless: true,
			translate: func() (any, *refError) {
				return t.streamBackends(kindUDPRoute, r.Namespace, firstRuleBackends(r.Spec.Rules, func(x gwv1.UDPRouteRule) []gwv1.BackendRef { return x.BackendRefs }), corev1.ProtocolUDP)
			},
		})
	}

	sortRouteInfos(out)

	return out
}

// firstRuleBackends returns the backends of all rules; stream routes have
// no matches, so their rules are equivalent.
func firstRuleBackends[R any](rules []R, get func(R) []gwv1.BackendRef) []gwv1.BackendRef {
	var out []gwv1.BackendRef
	for _, r := range rules {
		out = append(out, get(r)...)
	}

	return out
}
