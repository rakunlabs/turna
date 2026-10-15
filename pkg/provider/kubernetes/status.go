package kubernetes

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
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

	mu        sync.Mutex
	leader    bool
	leaderCtx context.Context
	pending   *statusUpdate
	wake      chan struct{}
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
		w.setLeader(ctx)
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
			OnStartedLeading: func(leaderCtx context.Context) {
				slog.Info("kubernetes provider: became status leader", "id", id)
				w.setLeader(leaderCtx)
			},
			OnStoppedLeading: func() {
				slog.Info("kubernetes provider: lost status leadership", "id", id)
				w.stopLeading()
			},
		},
	})

	return nil
}

func podNamespace() string {
	if ns := os.Getenv("POD_NAMESPACE"); ns != "" {
		return ns
	}

	if b, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace"); err == nil {
		if ns := strings.TrimSpace(string(b)); ns != "" {
			return ns
		}
	}

	return "default"
}

func (w *statusWriter) setLeader(ctx context.Context) {
	w.mu.Lock()
	w.leader = true
	w.leaderCtx = ctx
	w.mu.Unlock()

	// Reconcile and write the latest status after gaining leadership.
	w.p.notify()
}

func (w *statusWriter) stopLeading() {
	w.mu.Lock()
	w.leader = false
	w.leaderCtx = nil
	w.mu.Unlock()
}

func (w *statusWriter) leadership() (context.Context, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.leaderCtx, w.leader && w.leaderCtx != nil && w.leaderCtx.Err() == nil
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
		w.pending = nil
		w.mu.Unlock()

		leaderCtx, leader := w.leadership()
		if u == nil || !leader || w.p.cfg.Status.Disabled {
			continue
		}

		w.write(leaderCtx, u)
	}
}

func (w *statusWriter) write(ctx context.Context, u *statusUpdate) {
	controller := w.p.cfg.mode() == ModeController

	var addresses []gwv1.GatewayStatusAddress
	if !controller {
		addresses = w.addresses(ctx)
	}

	// GatewayClass status needs cluster-wide write access; a namespace scoped
	// installation leaves it to the cluster administrator.
	for _, gc := range u.snap.GatewayClasses {
		if w.p.cfg.NamespaceScoped {
			break
		}

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

		if err := gatewayError(u.applyErr, key(gw)); err != nil {
			st = withApplyError(st, err)
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
	w.writePolicies(ctx, u.snap, u.res)
}

// policyAncestors merges the ancestors of this controller into the current
// policy status and keeps entries of other controllers.
func policyAncestors(controller gwv1.GatewayController, st *gwv1.PolicyStatus, ancestors []gwv1.PolicyAncestorStatus) []gwv1.PolicyAncestorStatus {
	next := make([]gwv1.PolicyAncestorStatus, 0, len(st.Ancestors)+len(ancestors))
	for _, a := range st.Ancestors {
		if a.ControllerName != controller {
			next = append(next, a)
		}
	}

	for _, a := range ancestors {
		for _, old := range st.Ancestors {
			if old.ControllerName == controller && equality.Semantic.DeepEqual(old.AncestorRef, a.AncestorRef) {
				a.Conditions = mergeConditions(old.Conditions, a.Conditions)
			}
		}

		next = append(next, a)
	}

	return next
}

func (w *statusWriter) writePolicies(ctx context.Context, snap *Snapshot, res *Result) {
	api := w.p.gw.GatewayV1()
	controller := gwv1.GatewayController(w.p.cfg.controllerName())

	for _, source := range snap.BackendTLSPolicies {
		k := key(source)
		ancestors := res.PolicyAncestors(k)

		if equality.Semantic.DeepEqual(source.Status.Ancestors, policyAncestors(controller, &source.Status, ancestors)) {
			continue
		}

		err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			cur, err := api.BackendTLSPolicies(k.Namespace).Get(ctx, k.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}
			if !w.currentSnapshot(source, cur) {
				return nil
			}

			next := policyAncestors(controller, &cur.Status, ancestors)
			if equality.Semantic.DeepEqual(cur.Status.Ancestors, next) {
				return nil
			}

			cur.Status.Ancestors = next
			_, err = api.BackendTLSPolicies(k.Namespace).UpdateStatus(ctx, cur, metav1.UpdateOptions{})

			return err
		})
		if err != nil && !apierrors.IsNotFound(err) {
			slog.Warn("kubernetes provider: backend tls policy status", "policy", k.String(), "err", err.Error())
		}
	}
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

	var (
		svc *corev1.Service
		dep *appsv1.Deployment
		err error
	)

	if pl := w.p.proxies(); pl != nil {
		svc, err = pl.services.Services(ns).Get(name)
	} else {
		svc, err = w.p.kube.CoreV1().Services(ns).Get(ctx, name, metav1.GetOptions{})
	}
	if err != nil {
		return nil, false
	}
	if !w.ownsProxy(svc, gw) {
		return nil, false
	}

	if pl := w.p.proxies(); pl != nil {
		dep, err = pl.deployments.Deployments(ns).Get(name)
	} else {
		dep, err = w.p.kube.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
	}
	if err != nil {
		return nil, false
	}
	if !w.ownsProxy(dep, gw) {
		return nil, false
	}

	values := serviceAddresses(svc)
	if svc.Spec.Type == corev1.ServiceTypeLoadBalancer && len(svc.Status.LoadBalancer.Ingress) == 0 {
		// wait for the load balancer instead of publishing the cluster ip
		values = nil
	}

	return toAddresses(values), dep.Status.ObservedGeneration >= dep.Generation && dep.Status.ReadyReplicas > 0 && len(values) > 0
}

func (w *statusWriter) ownsProxy(obj metav1.Object, gw nsName) bool {
	lbls := obj.GetLabels()
	return lbls[labelManagedBy] == managedByValue && lbls[labelController] == w.p.controllerID() &&
		lbls[labelGatewayName] == truncLabel(gw.Name) && lbls[labelGatewayNamespace] == truncLabel(gw.Namespace)
}

// Status is computed from informer snapshots. A newer spec or a replacement
// object must be reconciled before any of that snapshot's status is written.
func (w *statusWriter) currentSnapshot(source, current metav1.Object) bool {
	if source.GetUID() != current.GetUID() || source.GetGeneration() != current.GetGeneration() {
		w.p.notify()
		return false
	}
	return true
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
		features.SupportBackendTLSPolicy,
		features.SupportBackendTLSPolicySANValidation,
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
		features.SupportHTTPRouteRequestPercentageMirror,
		features.SupportHTTPRouteRequestTimeout,
		features.SupportHTTPRouteBackendTimeout,
		features.SupportHTTPRouteBackendRequestHeaderModification,
		features.SupportHTTPRouteBackendProtocolH2C,
		features.SupportHTTPRouteBackendProtocolWebSocket,
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

func classStatus(cur *gwv1.GatewayClass, conds []metav1.Condition) *gwv1.GatewayClassStatus {
	next := cur.Status.DeepCopy()
	next.Conditions = mergeConditions(cur.Status.Conditions, conds)
	next.SupportedFeatures = supportedFeatures()

	return next
}

func (w *statusWriter) writeClass(ctx context.Context, gc *gwv1.GatewayClass, conds []metav1.Condition) {
	// The informer copy is current in the steady state; only a difference
	// costs API requests.
	if equality.Semantic.DeepEqual(gc.Status, *classStatus(gc, conds)) {
		return
	}

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		cur, err := w.p.gw.GatewayV1().GatewayClasses().Get(ctx, gc.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if !w.currentSnapshot(gc, cur) {
			return nil
		}

		next := classStatus(cur, conds)

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

func gatewayStatus(cur *gwv1.Gateway, st *GatewayStatus, addresses []gwv1.GatewayStatusAddress) *gwv1.GatewayStatus {
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

	return next
}

func (w *statusWriter) writeGateway(ctx context.Context, gw *gwv1.Gateway, st *GatewayStatus, addresses []gwv1.GatewayStatusAddress) {
	if equality.Semantic.DeepEqual(gw.Status, *gatewayStatus(gw, st, addresses)) {
		return
	}

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		cur, err := w.p.gw.GatewayV1().Gateways(gw.Namespace).Get(ctx, gw.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if !w.currentSnapshot(gw, cur) {
			return nil
		}

		next := gatewayStatus(cur, st, addresses)

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
type routeStatusClient[T metav1.Object] struct {
	get    func(ctx context.Context, ns, name string) (T, error)
	update func(ctx context.Context, obj T) error
	status func(obj T) *gwv1.RouteStatus
}

// routeParents merges the parents of this controller into the current
// route status and keeps entries of other controllers.
func routeParents(controller gwv1.GatewayController, st *gwv1.RouteStatus, parents []gwv1.RouteParentStatus) []gwv1.RouteParentStatus {
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

	return next
}

func writeRouteStatus[T metav1.Object](ctx context.Context, w *statusWriter, c routeStatusClient[T], kind gwv1.Kind, source T, parents []gwv1.RouteParentStatus) {
	controller := gwv1.GatewayController(w.p.cfg.controllerName())
	k := key(source)

	// Most routes are unchanged on a sync; skip them without a request.
	if st := c.status(source); equality.Semantic.DeepEqual(st.Parents, routeParents(controller, st, parents)) {
		return
	}

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		cur, err := c.get(ctx, k.Namespace, k.Name)
		if err != nil {
			return err
		}
		if !w.currentSnapshot(source, cur) {
			return nil
		}

		st := c.status(cur)
		next := routeParents(controller, st, parents)

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
		}, kindRoute, r, res.Routes[routeKey{Kind: kindRoute, nsName: key(r)}])
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
		}, kindGRPCRoute, r, res.Routes[routeKey{Kind: kindGRPCRoute, nsName: key(r)}])
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
		}, kindTLSRoute, r, res.Routes[routeKey{Kind: kindTLSRoute, nsName: key(r)}])
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
		}, kindTCPRoute, r, res.Routes[routeKey{Kind: kindTCPRoute, nsName: key(r)}])
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
		}, kindUDPRoute, r, res.Routes[routeKey{Kind: kindUDPRoute, nsName: key(r)}])
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
