package kubernetes

import (
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"

	shttp "github.com/rakunlabs/turna/pkg/server/http"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/gateway"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/service"
)

// grpcRules converts the rules of a GRPCRoute.
func (t *translator) grpcRules(route *gwv1.GRPCRoute) ([]gateway.Rule, *refError) {
	var firstErr *refError

	setErr := func(e *refError) {
		if firstErr == nil {
			firstErr = e
		}
	}

	out := make([]gateway.Rule, 0, len(route.Spec.Rules))

	for i, r := range route.Spec.Rules {
		rule := gateway.Rule{}

		matches, ok, err := validMatches(r.Matches, convertGRPCMatch, true)
		if err != nil {
			setErr(err)
		}
		if !ok {
			// no valid match left; an empty list would match every request
			continue
		}
		rule.Matches = matches

		filters, err := t.grpcFilters(route, r.Filters)
		if err != nil {
			setErr(err)

			rule.Status = http.StatusInternalServerError
			out = append(out, rule)

			continue
		}

		rule.Filters = filters

		for j, b := range r.BackendRefs {
			backend, err := t.grpcBackend(route, i, j, b)
			if err != nil {
				setErr(err)
			}

			rule.Backends = append(rule.Backends, backend)
		}

		out = append(out, rule)
	}

	return out, firstErr
}

func convertGRPCMatch(m gwv1.GRPCRouteMatch) gateway.Match {
	out := gateway.Match{}

	if m.Method != nil {
		gm := &gateway.GRPCMatch{
			Service: ptrOr(m.Method.Service, ""),
			Method:  ptrOr(m.Method.Method, ""),
			Type:    gateway.ValueExact,
		}

		if ptrOr(m.Method.Type, gwv1.GRPCMethodMatchExact) == gwv1.GRPCMethodMatchRegularExpression {
			gm.Type = gateway.ValueRegex
			// anchor regex matches to the whole name
			if gm.Service != "" {
				gm.Service = "^(?:" + gm.Service + ")$"
			}

			if gm.Method != "" {
				gm.Method = "^(?:" + gm.Method + ")$"
			}
		}

		out.GRPC = gm
	}

	for _, h := range m.Headers {
		out.Headers = append(out.Headers, gateway.ValueMatch{
			Name:  string(h.Name),
			Value: h.Value,
			Type:  valueType(ptrOr(h.Type, gwv1.GRPCHeaderMatchExact) == gwv1.GRPCHeaderMatchRegularExpression),
		})
	}

	return out
}

func (t *translator) grpcFilters(route *gwv1.GRPCRoute, in []gwv1.GRPCRouteFilter) ([]gateway.Filter, *refError) {
	out := make([]gateway.Filter, 0, len(in))

	for _, f := range in {
		switch f.Type {
		case gwv1.GRPCRouteFilterRequestHeaderModifier:
			out = append(out, gateway.Filter{RequestHeaders: headerModifier(f.RequestHeaderModifier)})
		case gwv1.GRPCRouteFilterResponseHeaderModifier:
			out = append(out, gateway.Filter{ResponseHeaders: headerModifier(f.ResponseHeaderModifier)})
		case gwv1.GRPCRouteFilterRequestMirror:
			m, err := t.requestMirror(kindGRPCRoute, route.Namespace, f.RequestMirror, true)
			if err != nil {
				return nil, err
			}

			out = append(out, gateway.Filter{Mirror: m})
		default:
			return nil, &refError{string(gwv1.RouteReasonUnsupportedValue), fmt.Sprintf("filter %s is not supported", f.Type)}
		}
	}

	return out, nil
}

func (t *translator) grpcBackend(route *gwv1.GRPCRoute, rule, idx int, b gwv1.GRPCBackendRef) (gateway.Backend, *refError) {
	out := gateway.Backend{Weight: int(ptrOr(b.Weight, 1))}

	addrs, scheme, err := t.resolveAddresses(kindGRPCRoute, route.Namespace, b.BackendObjectReference, corev1.ProtocolTCP)
	if err != nil {
		out.Status = http.StatusInternalServerError

		return out, err
	}

	if len(addrs) == 0 {
		out.Status = http.StatusServiceUnavailable

		return out, nil
	}

	var names []string

	if len(b.Filters) > 0 {
		filters, err := t.grpcFilters(route, b.Filters)
		if err != nil {
			out.Status = http.StatusInternalServerError

			return out, err
		}

		name := sanitize("k8s-grpc", route.Namespace, route.Name, strconv.Itoa(rule), strconv.Itoa(idx), "filters")
		t.middlewares[name] = shttp.HTTPMiddleware{Gateway: &gateway.Gateway{Passthrough: true, Rules: []gateway.Rule{{Filters: filters}}}}
		names = append(names, name)
	}

	urls := make([]string, 0, len(addrs))
	for _, a := range addrs {
		urls = append(urls, scheme+"://"+a)
	}

	// plain text gRPC needs HTTP/2 prior knowledge
	out.Middlewares = append(names, t.serviceMiddlewareFor(urls, scheme == "http"))

	return out, nil
}

// serviceMiddlewareFor returns a shared service middleware for the upstream
// set so routes using the same backend share health and balancing state.
func (t *translator) serviceMiddlewareFor(urls []string, h2c bool) string {
	sorted := slices.Clone(urls)
	slices.Sort(sorted)

	prefix := "k8s-svc"
	if h2c {
		prefix = "k8s-h2c"
	}

	name := sanitize(prefix, strings.Join(sorted, ","))

	if _, ok := t.middlewares[name]; ok {
		return name
	}

	passHost := true
	svc := &service.Service{
		PassHostHeader: &passHost,
		H2C:            h2c,
		LoadBalancer: service.LoadBalancer{
			Strategy:           t.cfg.Backend.Strategy,
			PassiveHealthCheck: &service.PassiveHealthCheck{MaxFails: 3},
		},
		Retry: ptrOr(t.cfg.Backend.Retry, 2),
	}

	for _, u := range sorted {
		svc.LoadBalancer.Servers = append(svc.LoadBalancer.Servers, service.Server{URL: u})
	}

	t.middlewares[name] = shttp.HTTPMiddleware{ServiceMiddleware: svc}

	return name
}
