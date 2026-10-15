package kubernetes

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"slices"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"

	shttp "github.com/rakunlabs/turna/pkg/server/http"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/gateway"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/service"
)

func validCertificate(c shttp.Certificate) error {
	_, err := tls.X509KeyPair([]byte(c.Cert), []byte(c.Key))

	return err
}

// rules converts the rules of a route. Invalid backends are reported in the
// returned error and answer 500 for their share of traffic.
func (t *translator) rules(route *gwv1.HTTPRoute) ([]gateway.Rule, *refError) {
	var firstErr *refError

	setErr := func(e *refError) {
		if firstErr == nil {
			firstErr = e
		}
	}

	out := make([]gateway.Rule, 0, len(route.Spec.Rules))

	for i, r := range route.Spec.Rules {
		rule := gateway.Rule{}

		if err := unsupportedRuleFields(r.SessionPersistence != nil); err != nil {
			setErr(err)
		}

		matches, ok, err := validMatches(r.Matches, convertMatch, false)
		if err != nil {
			setErr(err)
		}
		if !ok {
			// no valid match left; an empty list would match every request
			continue
		}
		rule.Matches = matches

		filters, err := t.filters(route, r.Filters)
		if err != nil {
			setErr(err)

			rule.Status = http.StatusInternalServerError
			out = append(out, rule)

			continue
		}

		rule.Filters = filters

		if r.Retry != nil {
			// each attempt is bounded by backendRequest, all of them by request
			rule.Timeout = parseTimeout(r.Timeouts, false)
			rule.BackendTimeout = parseTimeout(r.Timeouts, true)
			rule.Retry = convertRetry(r.Retry)
		} else {
			rule.Timeout = ruleTimeout(r.Timeouts)
		}

		redirect := slices.ContainsFunc(r.Filters, func(f gwv1.HTTPRouteFilter) bool {
			return f.Type == gwv1.HTTPRouteFilterRequestRedirect
		})

		if !redirect {
			for j, b := range r.BackendRefs {
				backend, err := t.backend(route, i, j, b, rule.Retry != nil)
				if err != nil {
					setErr(err)
				}

				rule.Backends = append(rule.Backends, backend)
			}
		}

		out = append(out, rule)
	}

	return out, firstErr
}

// unsupportedRuleFields reports rule fields that are not implemented. They
// do not change security or placement, so the rule keeps routing without
// them and only the status tells the route author.
func unsupportedRuleFields(sessionPersistence bool) *refError {
	if sessionPersistence {
		return &refError{string(gwv1.RouteReasonUnsupportedValue), "sessionPersistence is not supported"}
	}

	return nil
}

// convertRetry maps a route retry. Without attempts one retry is made, 0
// disables retries, without codes 502, 503 and 504 are retried, and without
// a backoff the next attempt starts right away.
func convertRetry(r *gwv1.HTTPRouteRetry) *gateway.Retry {
	out := &gateway.Retry{Attempts: new(ptrOr(r.Attempts, 1))}

	for _, c := range r.Codes {
		out.Codes = append(out.Codes, int(c))
	}

	if r.Backoff != nil {
		if d, err := time.ParseDuration(string(*r.Backoff)); err == nil && d > 0 {
			out.Backoff = d
		}
	}

	return out
}

// parseTimeout returns the request or backend request timeout of a rule, 0
// disables it.
func parseTimeout(t *gwv1.HTTPRouteTimeouts, backend bool) time.Duration {
	if t == nil {
		return 0
	}

	v := t.Request
	if backend {
		v = t.BackendRequest
	}

	if v == nil {
		return 0
	}

	d, err := time.ParseDuration(string(*v))
	if err != nil || d < 0 {
		return 0
	}

	return d
}

// ruleTimeout returns the timeout of a rule, 0 disables it. Retries go only
// to unreachable endpoints and fail fast, so the backend request timeout
// also bounds the whole request.
func ruleTimeout(t *gwv1.HTTPRouteTimeouts) time.Duration {
	if t == nil {
		return 0
	}

	var out time.Duration

	for _, v := range []*gwv1.Duration{t.Request, t.BackendRequest} {
		if v == nil {
			continue
		}

		if d, err := time.ParseDuration(string(*v)); err == nil && d > 0 && (out == 0 || d < out) {
			out = d
		}
	}

	return out
}

func convertMatch(m gwv1.HTTPRouteMatch) gateway.Match {
	out := gateway.Match{}

	if m.Path != nil {
		pm := &gateway.PathMatch{Type: gateway.PathPrefix, Value: ptrOr(m.Path.Value, "/")}

		switch ptrOr(m.Path.Type, gwv1.PathMatchPathPrefix) {
		case gwv1.PathMatchExact:
			pm.Type = gateway.PathExact
		case gwv1.PathMatchRegularExpression:
			pm.Type = gateway.PathRegex
		}

		out.Path = pm
	}

	if m.Method != nil {
		out.Method = string(*m.Method)
	}

	for _, h := range m.Headers {
		out.Headers = append(out.Headers, gateway.ValueMatch{
			Name:  string(h.Name),
			Value: h.Value,
			Type:  valueType(ptrOr(h.Type, gwv1.HeaderMatchExact) == gwv1.HeaderMatchRegularExpression),
		})
	}

	for _, q := range m.QueryParams {
		out.Query = append(out.Query, gateway.ValueMatch{
			Name:  string(q.Name),
			Value: q.Value,
			Type:  valueType(ptrOr(q.Type, gwv1.QueryParamMatchExact) == gwv1.QueryParamMatchRegularExpression),
		})
	}

	return out
}

func valueType(regex bool) string {
	if regex {
		return gateway.ValueRegex
	}

	return gateway.ValueExact
}

// validMatches converts route matches and drops matches the gateway cannot
// compile, such as unsupported regular expressions. One invalid match must
// not fail the whole configuration of every route. ok is false when the rule
// had matches and none is left.
func validMatches[M any](in []M, convert func(M) gateway.Match, grpc bool) (out []gateway.Match, ok bool, firstErr *refError) {
	for _, m := range in {
		gm := convert(m)
		if err := gateway.ValidateMatch(gm, grpc); err != nil {
			if firstErr == nil {
				firstErr = &refError{string(gwv1.RouteReasonUnsupportedValue), fmt.Sprintf("match is not supported: %v", err)}
			}

			continue
		}

		out = append(out, gm)
	}

	return out, len(in) == 0 || len(out) > 0, firstErr
}

func (t *translator) filters(route *gwv1.HTTPRoute, in []gwv1.HTTPRouteFilter) ([]gateway.Filter, *refError) {
	out := make([]gateway.Filter, 0, len(in))

	for _, f := range in {
		switch f.Type {
		case gwv1.HTTPRouteFilterRequestHeaderModifier:
			out = append(out, gateway.Filter{RequestHeaders: headerModifier(f.RequestHeaderModifier)})
		case gwv1.HTTPRouteFilterResponseHeaderModifier:
			out = append(out, gateway.Filter{ResponseHeaders: headerModifier(f.ResponseHeaderModifier)})
		case gwv1.HTTPRouteFilterRequestRedirect:
			out = append(out, gateway.Filter{Redirect: redirect(f.RequestRedirect)})
		case gwv1.HTTPRouteFilterURLRewrite:
			out = append(out, gateway.Filter{Rewrite: rewrite(f.URLRewrite)})
		case gwv1.HTTPRouteFilterRequestMirror:
			m, err := t.mirror(route, f.RequestMirror)
			if err != nil {
				return nil, err
			}

			out = append(out, gateway.Filter{Mirror: m})
		case gwv1.HTTPRouteFilterCORS:
			out = append(out, gateway.Filter{CORS: cors(f.CORS)})
		default:
			return nil, &refError{string(gwv1.RouteReasonUnsupportedValue), fmt.Sprintf("filter %s is not supported", f.Type)}
		}
	}

	return out, nil
}

func cors(c *gwv1.HTTPCORSFilter) *gateway.CORS {
	if c == nil {
		return &gateway.CORS{}
	}

	out := &gateway.CORS{
		AllowCredentials: ptrOr(c.AllowCredentials, false),
		MaxAge:           int(c.MaxAge),
	}

	for _, o := range c.AllowOrigins {
		out.AllowOrigins = append(out.AllowOrigins, string(o))
	}

	for _, m := range c.AllowMethods {
		out.AllowMethods = append(out.AllowMethods, string(m))
	}

	for _, h := range c.AllowHeaders {
		out.AllowHeaders = append(out.AllowHeaders, string(h))
	}

	for _, h := range c.ExposeHeaders {
		out.ExposeHeaders = append(out.ExposeHeaders, string(h))
	}

	return out
}

func headerModifier(h *gwv1.HTTPHeaderFilter) *gateway.HeaderModifier {
	if h == nil {
		return &gateway.HeaderModifier{}
	}

	out := &gateway.HeaderModifier{Remove: h.Remove}

	if len(h.Set) > 0 {
		out.Set = make(map[string]string, len(h.Set))
		for _, v := range h.Set {
			out.Set[string(v.Name)] = v.Value
		}
	}

	if len(h.Add) > 0 {
		out.Add = make(map[string]string, len(h.Add))
		for _, v := range h.Add {
			out.Add[string(v.Name)] = v.Value
		}
	}

	return out
}

func pathModifier(p *gwv1.HTTPPathModifier) *gateway.PathModifier {
	if p == nil {
		return nil
	}

	switch p.Type {
	case gwv1.FullPathHTTPPathModifier:
		return &gateway.PathModifier{ReplaceFullPath: p.ReplaceFullPath}
	case gwv1.PrefixMatchHTTPPathModifier:
		return &gateway.PathModifier{ReplacePrefixMatch: p.ReplacePrefixMatch}
	default:
		return nil
	}
}

func redirect(r *gwv1.HTTPRequestRedirectFilter) *gateway.Redirect {
	if r == nil {
		return &gateway.Redirect{}
	}

	out := &gateway.Redirect{
		Scheme:     ptrOr(r.Scheme, ""),
		Hostname:   string(ptrOr(r.Hostname, "")),
		Port:       int(ptrOr(r.Port, 0)),
		StatusCode: ptrOr(r.StatusCode, http.StatusFound),
		Path:       pathModifier(r.Path),
	}

	return out
}

func rewrite(r *gwv1.HTTPURLRewriteFilter) *gateway.Rewrite {
	if r == nil {
		return &gateway.Rewrite{}
	}

	return &gateway.Rewrite{
		Hostname: string(ptrOr(r.Hostname, "")),
		Path:     pathModifier(r.Path),
	}
}

func (t *translator) mirror(route *gwv1.HTTPRoute, m *gwv1.HTTPRequestMirrorFilter) (*service.Mirror, *refError) {
	return t.requestMirror(kindRoute, route.Namespace, m, false)
}

// requestMirror keeps a Service's endpoints in one target so each sampled
// request is sent to exactly one endpoint, regardless of the route kind.
func (t *translator) requestMirror(kind gwv1.Kind, namespace string, m *gwv1.HTTPRequestMirrorFilter, grpc bool) (*service.Mirror, *refError) {
	if m == nil {
		return nil, &refError{string(gwv1.RouteReasonUnsupportedValue), "requestMirror is empty"}
	}

	up, err := t.resolveAddresses(kind, namespace, m.BackendRef, corev1.ProtocolTCP)
	if err != nil {
		return nil, err
	}
	urls := up.urls()

	percent := 100.0

	switch {
	case m.Percent != nil:
		percent = float64(*m.Percent)
	case m.Fraction != nil:
		den := ptrOr(m.Fraction.Denominator, 100)
		if den > 0 {
			percent = float64(m.Fraction.Numerator) * 100 / float64(den)
		}
	}

	out := &service.Mirror{H2C: up.scheme == "http" && (grpc || t.backendH2C(namespace, m.BackendRef)), TLS: up.tls}
	if len(urls) > 0 {
		out.Servers = []service.MirrorServer{{Endpoints: urls, Percent: &percent}}
	}

	return out, nil
}

func (t *translator) backend(route *gwv1.HTTPRoute, rule, idx int, b gwv1.HTTPBackendRef, routeRetry bool) (gateway.Backend, *refError) {
	weight := int(ptrOr(b.Weight, 1))
	out := gateway.Backend{Weight: weight}

	up, err := t.resolveAddresses(kindRoute, route.Namespace, b.BackendObjectReference, corev1.ProtocolTCP)
	if err != nil {
		out.Status = http.StatusInternalServerError

		return out, err
	}

	if len(up.addrs) == 0 {
		out.Status = http.StatusServiceUnavailable

		return out, nil
	}

	var filterNames []string

	if len(b.Filters) > 0 {
		filters, err := t.filters(route, b.Filters)
		if err != nil {
			out.Status = http.StatusInternalServerError

			return out, err
		}

		name := sanitize("k8s", route.Namespace, route.Name, strconv.Itoa(rule), strconv.Itoa(idx), "filters")
		t.middlewares[name] = shttp.HTTPMiddleware{Gateway: &gateway.Gateway{Passthrough: true, Rules: []gateway.Rule{{Filters: filters}}}}
		filterNames = append(filterNames, name)
	}

	name := t.serviceMiddlewareFor(up, up.scheme == "http" && t.backendH2C(route.Namespace, b.BackendObjectReference), routeRetry)
	out.Middlewares = append(filterNames, name)

	return out, nil
}

// backendH2C is called only after resolving a permitted Service reference.
func (t *translator) backendH2C(namespace string, ref gwv1.BackendObjectReference) bool {
	ns := string(ptrOr(ref.Namespace, gwv1.Namespace(namespace)))
	svc := t.idx.services[nsName{Namespace: ns, Name: string(ref.Name)}]
	if svc == nil || ref.Port == nil {
		return false
	}
	for _, p := range svc.Spec.Ports {
		if p.Port == int32(*ref.Port) && (p.Protocol == "" || p.Protocol == corev1.ProtocolTCP) {
			return ptrOr(p.AppProtocol, "") == "kubernetes.io/h2c"
		}
	}
	return false
}

// upstream is a resolved Service port.
type upstream struct {
	addrs  []string
	scheme string
	// tls verifies https upstreams, set by a BackendTLSPolicy
	tls *service.UpstreamTLS
}

func (u upstream) urls() []string {
	out := make([]string, 0, len(u.addrs))
	for _, a := range u.addrs {
		out = append(out, u.scheme+"://"+a)
	}

	return out
}

// resolveAddresses returns "host:port" upstreams of a Service reference made
// by a route of fromKind in fromNamespace, and the scheme derived from the
// port app protocol. A BackendTLSPolicy targeting the port switches it to
// verified TLS.
func (t *translator) resolveAddresses(fromKind gwv1.Kind, fromNamespace string, ref gwv1.BackendObjectReference, protocol corev1.Protocol) (upstream, *refError) {
	if ptrOr(ref.Group, "") != "" || ptrOr(ref.Kind, kindService) != kindService {
		return upstream{}, &refError{string(gwv1.RouteReasonInvalidKind), fmt.Sprintf("backend kind %s is not supported", ptrOr(ref.Kind, ""))}
	}

	ns := string(ptrOr(ref.Namespace, gwv1.Namespace(fromNamespace)))
	if !t.idx.referenceAllowed(groupGateway, string(fromKind), fromNamespace, "", kindService, ns, string(ref.Name)) {
		return upstream{}, &refError{string(gwv1.RouteReasonRefNotPermitted), fmt.Sprintf("service %s/%s is not permitted by a ReferenceGrant", ns, ref.Name)}
	}

	svc, ok := t.idx.services[nsName{Namespace: ns, Name: string(ref.Name)}]
	if !ok {
		return upstream{}, &refError{string(gwv1.RouteReasonBackendNotFound), fmt.Sprintf("service %s/%s not found", ns, ref.Name)}
	}

	if ref.Port == nil {
		return upstream{}, &refError{string(gwv1.RouteReasonUnsupportedValue), fmt.Sprintf("service %s/%s: port is required", ns, ref.Name)}
	}

	var port *corev1.ServicePort
	for i := range svc.Spec.Ports {
		p := &svc.Spec.Ports[i]
		if p.Port == int32(*ref.Port) && (p.Protocol == "" && protocol == corev1.ProtocolTCP || p.Protocol == protocol) {
			port = p

			break
		}
	}

	if port == nil {
		return upstream{}, &refError{string(gwv1.RouteReasonBackendNotFound), fmt.Sprintf("service %s/%s has no %s port %d", ns, ref.Name, protocol, *ref.Port)}
	}

	out := upstream{scheme: "http"}
	if port.AppProtocol != nil {
		switch *port.AppProtocol {
		case "kubernetes.io/h2c", "kubernetes.io/ws":
		case "kubernetes.io/wss", "https":
			out.scheme = "https"
		}
	}

	// stream routes pass connections through unchanged
	if fromKind == kindRoute || fromKind == kindGRPCRoute {
		if cfg := t.upstreamTLS(svc, port); cfg != nil {
			out.scheme, out.tls = "https", cfg
		}
	}

	if svc.Spec.Type == corev1.ServiceTypeExternalName {
		if !t.cfg.Backend.AllowExternalName {
			return upstream{}, &refError{string(gwv1.RouteReasonUnsupportedValue), fmt.Sprintf("service %s/%s: ExternalName services are disabled (backend.allow_external_name)", ns, ref.Name)}
		}

		out.addrs = []string{net.JoinHostPort(svc.Spec.ExternalName, strconv.Itoa(int(port.Port)))}

		return out, nil
	}

	if t.cfg.Backend.UseServiceIP {
		if svc.Spec.ClusterIP == "" || svc.Spec.ClusterIP == corev1.ClusterIPNone {
			return upstream{}, &refError{string(gwv1.RouteReasonBackendNotFound), fmt.Sprintf("service %s/%s has no cluster ip", ns, ref.Name)}
		}

		out.addrs = []string{net.JoinHostPort(svc.Spec.ClusterIP, strconv.Itoa(int(port.Port)))}

		return out, nil
	}

	out.addrs = t.endpoints(nsName{Namespace: ns, Name: svc.Name}, port, protocol)

	return out, nil
}

func (t *translator) endpoints(svc nsName, port *corev1.ServicePort, protocol corev1.Protocol) []string {
	var addrs []string

	for _, slice := range t.idx.slices[svc] {
		if slice.AddressType != discoveryv1.AddressTypeIPv4 && slice.AddressType != discoveryv1.AddressTypeIPv6 {
			continue
		}

		var target *int32

		for _, p := range slice.Ports {
			pp := ptrOr(p.Protocol, corev1.ProtocolTCP)
			if ptrOr(p.Name, "") == port.Name && pp == protocol {
				target = p.Port

				break
			}
		}

		if target == nil {
			continue
		}

		for _, ep := range slice.Endpoints {
			if !readyEndpoint(ep) {
				continue
			}

			for _, addr := range ep.Addresses {
				addrs = append(addrs, net.JoinHostPort(addr, strconv.Itoa(int(*target))))
			}
		}
	}

	slices.Sort(addrs)

	return slices.Compact(addrs)
}
