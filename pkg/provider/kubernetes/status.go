package kubernetes

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"slices"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/util/retry"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
	"sigs.k8s.io/gateway-api/pkg/features"
)

type statusWriter struct {
	p *Provider

	mu      sync.Mutex
	leader  bool
	pending *statusUpdate
	wake    chan struct{}
}

type statusUpdate struct {
	snap     *Snapshot
	res      *Result
	applyErr error
}

func newStatusWriter(p *Provider) *statusWriter {
	return &statusWriter{p: p, wake: make(chan struct{}, 1)}
}

func (w *statusWriter) update(snap *Snapshot, res *Result, applyErr error) {
	w.mu.Lock()
	w.pending = &statusUpdate{snap: snap, res: res, applyErr: applyErr}
	w.mu.Unlock()

	select {
	case w.wake <- struct{}{}:
	default:
	}
}

func (w *statusWriter) run(ctx context.Context) {
	le := w.p.cfg.Status.LeaderElection
	if le.Disabled {
		w.setLeader(true)
		w.loop(ctx)

		return
	}

	go w.loop(ctx)

	for ctx.Err() == nil {
		if err := w.elect(ctx); err != nil {
			slog.Error("kubernetes provider: leader election", "err", err.Error())

			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
		}
	}
}

func (w *statusWriter) elect(ctx context.Context) error {
	le := w.p.cfg.Status.LeaderElection

	ns := le.Namespace
	if ns == "" {
		ns = podNamespace()
	}

	name := le.Name
	if name == "" {
		name = "turna-gateway"
	}

	id, _ := os.Hostname()
	if id == "" {
		id = fmt.Sprintf("turna-%d", time.Now().UnixNano())
	}

	lock, err := resourcelock.New(resourcelock.LeasesResourceLock, ns, name,
		w.p.kube.CoreV1(), w.p.kube.CoordinationV1(), resourcelock.ResourceLockConfig{Identity: id})
	if err != nil {
		return err
	}

	leaderelection.RunOrDie(ctx, leaderelection.LeaderElectionConfig{
		Lock:            lock,
		LeaseDuration:   15 * time.Second,
		RenewDeadline:   10 * time.Second,
		RetryPeriod:     2 * time.Second,
		ReleaseOnCancel: true,
		Name:            name,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(context.Context) {
				slog.Info("kubernetes provider: became status leader", "id", id)
				w.setLeader(true)
			},
			OnStoppedLeading: func() {
				slog.Info("kubernetes provider: lost status leadership", "id", id)
				w.setLeader(false)
			},
		},
	})

	return nil
}

func podNamespace() string {
	if ns := os.Getenv("POD_NAMESPACE"); ns != "" {
		return ns
	}

	if b, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace"); err == nil && len(b) > 0 {
		return string(b)
	}

	return "default"
}

func (w *statusWriter) setLeader(v bool) {
	w.mu.Lock()
	w.leader = v
	w.mu.Unlock()

	if v {
		// write the latest status after gaining leadership
		w.p.notify()
	}
}

func (w *statusWriter) loop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.wake:
		}

		w.mu.Lock()
		u := w.pending
		leader := w.leader
		w.pending = nil
		w.mu.Unlock()

		if u == nil || !leader {
			continue
		}

		w.write(ctx, u)
	}
}

func (w *statusWriter) write(ctx context.Context, u *statusUpdate) {
	controller := w.p.cfg.mode() == ModeController

	var addresses []gwv1.GatewayStatusAddress
	if !controller {
		addresses = w.addresses(ctx)
	}

	for _, gc := range u.snap.GatewayClasses {
		conds, ok := u.res.GatewayClasses[gc.Name]
		if !ok {
			continue
		}

		w.writeClass(ctx, gc, conds)
	}

	for _, gw := range u.snap.Gateways {
		st, ok := u.res.Gateways[key(gw)]
		if !ok {
			continue
		}

		if u.applyErr != nil {
			st = withApplyError(st, u.applyErr)
		}

		gwAddresses := addresses
		if controller {
			var ready bool

			gwAddresses, ready = w.proxyStatus(ctx, key(gw))
			if !ready {
				st = withNotReady(st)
			}
		}

		w.writeGateway(ctx, gw, st, gwAddresses)
	}

	w.writeRoutes(ctx, u.snap, u.res)
}

func withApplyError(st *GatewayStatus, err error) *GatewayStatus {
	out := *st
	out.Conditions = slices.Clone(st.Conditions)

	for i := range out.Conditions {
		if out.Conditions[i].Type == string(gwv1.GatewayConditionProgrammed) {
			out.Conditions[i].Status = metav1.ConditionFalse
			out.Conditions[i].Reason = string(gwv1.GatewayReasonInvalid)
			out.Conditions[i].Message = err.Error()
		}
	}

	return &out
}

// proxyStatus returns the addresses of the gateway Service and whether the
// proxy has a ready replica.
func (w *statusWriter) proxyStatus(ctx context.Context, gw nsName) ([]gwv1.GatewayStatusAddress, bool) {
	ns := w.p.proxyNamespace()
	name := proxyName(gw)

	svc, err := w.p.kube.CoreV1().Services(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, false
	}

	dep, err := w.p.kube.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, false
	}

	values := serviceAddresses(svc)
	if svc.Spec.Type == corev1.ServiceTypeLoadBalancer && len(svc.Status.LoadBalancer.Ingress) == 0 {
		// wait for the load balancer instead of publishing the cluster ip
		values = nil
	}

	return toAddresses(values), dep.Status.ReadyReplicas > 0 && len(values) > 0
}

func withNotReady(st *GatewayStatus) *GatewayStatus {
	out := *st
	out.Conditions = slices.Clone(st.Conditions)

	for i := range out.Conditions {
		c := &out.Conditions[i]
		if c.Type == string(gwv1.GatewayConditionProgrammed) && c.Status == metav1.ConditionTrue {
			c.Status = metav1.ConditionFalse
			c.Reason = string(gwv1.GatewayReasonPending)
			c.Message = "waiting for the proxy deployment and address"
		}
	}

	return &out
}

// mergeConditions keeps LastTransitionTime of unchanged conditions.
func mergeConditions(existing, next []metav1.Condition) []metav1.Condition {
	out := make([]metav1.Condition, 0, len(next))

	for _, c := range next {
		if old := meta.FindStatusCondition(existing, c.Type); old != nil && old.Status == c.Status {
			c.LastTransitionTime = old.LastTransitionTime
		}

		out = append(out, c)
	}

	return out
}

func supportedFeatures() []gwv1.SupportedFeature {
	names := []features.FeatureName{
		features.SupportGateway,
		features.SupportHTTPRoute,
		features.SupportReferenceGrant,
		features.SupportHTTPRouteQueryParamMatching,
		features.SupportHTTPRouteMethodMatching,
		features.SupportHTTPRouteResponseHeaderModification,
		features.SupportHTTPRoutePortRedirect,
		features.SupportHTTPRouteSchemeRedirect,
		features.SupportHTTPRoutePathRedirect,
		features.SupportHTTPRouteHostRewrite,
		features.SupportHTTPRoutePathRewrite,
		features.SupportHTTPRouteRequestMirror,
		features.SupportHTTPRouteRequestMultipleMirrors,
		features.SupportHTTPRouteRequestTimeout,
		features.SupportHTTPRouteParentRefPort,
		features.SupportHTTPRoute303RedirectStatusCode,
		features.SupportHTTPRoute307RedirectStatusCode,
		features.SupportHTTPRoute308RedirectStatusCode,
		features.SupportGatewayPort8080,
		features.SupportGRPCRoute,
		features.SupportGRPCRouteNamedRouteRule,
		features.SupportHTTPRouteNamedRouteRule,
		features.SupportTLSRoute,
		features.SupportTLSRouteModeTerminate,
		features.SupportTLSRouteModeMixed,
		features.SupportTCPRoute,
		features.SupportUDPRoute,
	}

	out := make([]gwv1.SupportedFeature, 0, len(names))
	for _, n := range names {
		out = append(out, gwv1.SupportedFeature{Name: gwv1.FeatureName(n)})
	}

	slices.SortFunc(out, func(a, b gwv1.SupportedFeature) int {
		if a.Name < b.Name {
			return -1
		}

		if a.Name > b.Name {
			return 1
		}

		return 0
	})

	return out
}

func (w *statusWriter) writeClass(ctx context.Context, gc *gwv1.GatewayClass, conds []metav1.Condition) {
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		cur, err := w.p.gw.GatewayV1().GatewayClasses().Get(ctx, gc.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		next := cur.Status.DeepCopy()
		next.Conditions = mergeConditions(cur.Status.Conditions, conds)
		next.SupportedFeatures = supportedFeatures()

		if equality.Semantic.DeepEqual(cur.Status, *next) {
			return nil
		}

		cur.Status = *next
		_, err = w.p.gw.GatewayV1().GatewayClasses().UpdateStatus(ctx, cur, metav1.UpdateOptions{})

		return err
	})
	if err != nil && !apierrors.IsNotFound(err) {
		slog.Warn("kubernetes provider: gatewayclass status", "name", gc.Name, "err", err.Error())
	}
}

func (w *statusWriter) writeGateway(ctx context.Context, gw *gwv1.Gateway, st *GatewayStatus, addresses []gwv1.GatewayStatusAddress) {
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		cur, err := w.p.gw.GatewayV1().Gateways(gw.Namespace).Get(ctx, gw.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		next := cur.Status.DeepCopy()
		next.Conditions = mergeConditions(cur.Status.Conditions, st.Conditions)
		next.Addresses = addresses

		listeners := make([]gwv1.ListenerStatus, 0, len(st.Listeners))
		for _, l := range st.Listeners {
			for _, old := range cur.Status.Listeners {
				if old.Name == l.Name {
					l.Conditions = mergeConditions(old.Conditions, l.Conditions)
				}
			}

			listeners = append(listeners, l)
		}

		next.Listeners = listeners

		if equality.Semantic.DeepEqual(cur.Status, *next) {
			return nil
		}

		cur.Status = *next
		_, err = w.p.gw.GatewayV1().Gateways(gw.Namespace).UpdateStatus(ctx, cur, metav1.UpdateOptions{})

		return err
	})
	if err != nil && !apierrors.IsNotFound(err) {
		slog.Warn("kubernetes provider: gateway status", "gateway", key(gw).String(), "err", err.Error())
	}
}

// routeStatusClient reads and writes the parent status of one route kind.
type routeStatusClient[T any] struct {
	get    func(ctx context.Context, ns, name string) (T, error)
	update func(ctx context.Context, obj T) error
	status func(obj T) *gwv1.RouteStatus
}

func writeRouteStatus[T any](ctx context.Context, w *statusWriter, c routeStatusClient[T], kind gwv1.Kind, k nsName, parents []gwv1.RouteParentStatus) {
	controller := gwv1.GatewayController(w.p.cfg.controllerName())

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		cur, err := c.get(ctx, k.Namespace, k.Name)
		if err != nil {
			return err
		}

		st := c.status(cur)

		// keep entries of other controllers
		next := make([]gwv1.RouteParentStatus, 0, len(st.Parents)+len(parents))
		for _, p := range st.Parents {
			if p.ControllerName != controller {
				next = append(next, p)
			}
		}

		for _, p := range parents {
			for _, old := range st.Parents {
				if old.ControllerName == controller && equality.Semantic.DeepEqual(old.ParentRef, p.ParentRef) {
					p.Conditions = mergeConditions(old.Conditions, p.Conditions)
				}
			}

			next = append(next, p)
		}

		if equality.Semantic.DeepEqual(st.Parents, next) {
			return nil
		}

		st.Parents = next

		return c.update(ctx, cur)
	})
	if err != nil && !apierrors.IsNotFound(err) {
		slog.Warn("kubernetes provider: route status", "kind", kind, "route", k.String(), "err", err.Error())
	}
}

func (w *statusWriter) writeRoutes(ctx context.Context, snap *Snapshot, res *Result) {
	api := w.p.gw.GatewayV1()

	for _, r := range snap.HTTPRoutes {
		writeRouteStatus(ctx, w, routeStatusClient[*gwv1.HTTPRoute]{
			get: func(ctx context.Context, ns, name string) (*gwv1.HTTPRoute, error) {
				return api.HTTPRoutes(ns).Get(ctx, name, metav1.GetOptions{})
			},
			update: func(ctx context.Context, o *gwv1.HTTPRoute) error {
				_, err := api.HTTPRoutes(o.Namespace).UpdateStatus(ctx, o, metav1.UpdateOptions{})

				return err
			},
			status: func(o *gwv1.HTTPRoute) *gwv1.RouteStatus { return &o.Status.RouteStatus },
		}, kindRoute, key(r), res.Routes[routeKey{Kind: kindRoute, nsName: key(r)}])
	}

	for _, r := range snap.GRPCRoutes {
		writeRouteStatus(ctx, w, routeStatusClient[*gwv1.GRPCRoute]{
			get: func(ctx context.Context, ns, name string) (*gwv1.GRPCRoute, error) {
				return api.GRPCRoutes(ns).Get(ctx, name, metav1.GetOptions{})
			},
			update: func(ctx context.Context, o *gwv1.GRPCRoute) error {
				_, err := api.GRPCRoutes(o.Namespace).UpdateStatus(ctx, o, metav1.UpdateOptions{})

				return err
			},
			status: func(o *gwv1.GRPCRoute) *gwv1.RouteStatus { return &o.Status.RouteStatus },
		}, kindGRPCRoute, key(r), res.Routes[routeKey{Kind: kindGRPCRoute, nsName: key(r)}])
	}

	for _, r := range snap.TLSRoutes {
		writeRouteStatus(ctx, w, routeStatusClient[*gwv1.TLSRoute]{
			get: func(ctx context.Context, ns, name string) (*gwv1.TLSRoute, error) {
				return api.TLSRoutes(ns).Get(ctx, name, metav1.GetOptions{})
			},
			update: func(ctx context.Context, o *gwv1.TLSRoute) error {
				_, err := api.TLSRoutes(o.Namespace).UpdateStatus(ctx, o, metav1.UpdateOptions{})

				return err
			},
			status: func(o *gwv1.TLSRoute) *gwv1.RouteStatus { return &o.Status.RouteStatus },
		}, kindTLSRoute, key(r), res.Routes[routeKey{Kind: kindTLSRoute, nsName: key(r)}])
	}

	for _, r := range snap.TCPRoutes {
		writeRouteStatus(ctx, w, routeStatusClient[*gwv1.TCPRoute]{
			get: func(ctx context.Context, ns, name string) (*gwv1.TCPRoute, error) {
				return api.TCPRoutes(ns).Get(ctx, name, metav1.GetOptions{})
			},
			update: func(ctx context.Context, o *gwv1.TCPRoute) error {
				_, err := api.TCPRoutes(o.Namespace).UpdateStatus(ctx, o, metav1.UpdateOptions{})

				return err
			},
			status: func(o *gwv1.TCPRoute) *gwv1.RouteStatus { return &o.Status.RouteStatus },
		}, kindTCPRoute, key(r), res.Routes[routeKey{Kind: kindTCPRoute, nsName: key(r)}])
	}

	for _, r := range snap.UDPRoutes {
		writeRouteStatus(ctx, w, routeStatusClient[*gwv1.UDPRoute]{
			get: func(ctx context.Context, ns, name string) (*gwv1.UDPRoute, error) {
				return api.UDPRoutes(ns).Get(ctx, name, metav1.GetOptions{})
			},
			update: func(ctx context.Context, o *gwv1.UDPRoute) error {
				_, err := api.UDPRoutes(o.Namespace).UpdateStatus(ctx, o, metav1.UpdateOptions{})

				return err
			},
			status: func(o *gwv1.UDPRoute) *gwv1.RouteStatus { return &o.Status.RouteStatus },
		}, kindUDPRoute, key(r), res.Routes[routeKey{Kind: kindUDPRoute, nsName: key(r)}])
	}
}

// addresses returns the gateway addresses from config or the exposing
// LoadBalancer service.
func (w *statusWriter) addresses(ctx context.Context) []gwv1.GatewayStatusAddress {
	var values []string

	values = append(values, w.p.cfg.Status.Addresses...)

	if len(values) == 0 && w.p.cfg.Status.Service != "" {
		if k, ok := splitNsName(w.p.cfg.Status.Service); ok {
			svc, err := w.p.kube.CoreV1().Services(k.Namespace).Get(ctx, k.Name, metav1.GetOptions{})
			if err != nil {
				slog.Warn("kubernetes provider: status service", "service", w.p.cfg.Status.Service, "err", err.Error())
			} else {
				values = serviceAddresses(svc)
			}
		}
	}

	return toAddresses(values)
}

func toAddresses(values []string) []gwv1.GatewayStatusAddress {
	out := make([]gwv1.GatewayStatusAddress, 0, len(values))

	for _, v := range values {
		typ := gwv1.HostnameAddressType
		if net.ParseIP(v) != nil {
			typ = gwv1.IPAddressType
		}

		out = append(out, gwv1.GatewayStatusAddress{Type: &typ, Value: v})
	}

	return out
}

func serviceAddresses(svc *corev1.Service) []string {
	var out []string

	for _, ing := range svc.Status.LoadBalancer.Ingress {
		switch {
		case ing.IP != "":
			out = append(out, ing.IP)
		case ing.Hostname != "":
			out = append(out, ing.Hostname)
		}
	}

	if len(out) == 0 {
		out = append(out, svc.Spec.ExternalIPs...)
	}

	if len(out) == 0 && svc.Spec.ClusterIP != "" && svc.Spec.ClusterIP != corev1.ClusterIPNone {
		out = append(out, svc.Spec.ClusterIP)
	}

	return out
}
