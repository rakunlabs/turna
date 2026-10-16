package kubernetes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/goccy/go-yaml"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
)

const (
	labelManagedBy        = "app.kubernetes.io/managed-by"
	labelGatewayName      = "gateway.networking.k8s.io/gateway-name"
	labelGatewayNamespace = "turna.rakunlabs.com/gateway-namespace"
	labelController       = "turna.rakunlabs.com/controller-id"
	annotationConfigHash  = "turna.rakunlabs.com/config-hash"
	// turnaPrefix starts the labels and annotations managed by turna
	turnaPrefix       = "turna.rakunlabs.com/"
	annotationManaged = "turna.rakunlabs.com/managed-annotations"
	// annotationLoadBalancerIP is the spec.loadBalancerIP set from Gateway
	// spec.addresses
	annotationLoadBalancerIP = "turna.rakunlabs.com/load-balancer-ip"

	managedByValue = "turna-gateway"
	proxyContainer = "turna"
)

// proxyName is the name of the resources of a gateway, unique across
// namespaces as resources live in the controller namespace.
func proxyName(gw nsName) string {
	// Gateway names are DNS subdomains, Service names must be DNS labels.
	name := strings.ReplaceAll("turna-"+gw.Namespace+"-"+gw.Name, ".", "-")
	sum := sha256.Sum256([]byte(gw.String()))
	if len(name) > 52 {
		name = name[:52]
	}
	return strings.TrimRight(name, "-.") + "-" + hex.EncodeToString(sum[:])[:10]
}

// controllerID is safe to use as a label value and independent of ordering
// in namespace and gateway scope configuration.
func (p *Provider) controllerID() string {
	id := p.cfg.ControllerID
	if id == "" {
		namespaces := slices.Clone(p.cfg.Namespaces)
		gateways := slices.Clone(p.cfg.Gateways)
		slices.Sort(namespaces)
		slices.Sort(gateways)
		id = fmt.Sprintf("%s\x00%q\x00%q\x00%s\x00%s", p.cfg.controllerName(), namespaces, gateways,
			p.cfg.Status.LeaderElection.Namespace, p.cfg.Status.LeaderElection.Name)
		if p.cfg.GatewayClass != "" {
			id += "\x00" + p.cfg.GatewayClass
		}
	}
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:])[:32]
}

func proxyLabels(gw nsName) map[string]string {
	return map[string]string{
		labelManagedBy:               managedByValue,
		labelGatewayName:             truncLabel(gw.Name),
		labelGatewayNamespace:        truncLabel(gw.Namespace),
		"app.kubernetes.io/name":     "turna-gateway-proxy",
		"app.kubernetes.io/instance": truncLabel(proxyName(gw)),
	}
}

func truncLabel(v string) string {
	if len(v) > 63 {
		return strings.TrimRight(v[:63], "-_.")
	}

	return v
}

// proxyConfig is the turna configuration file of a gateway proxy.
func (c *Config) proxyConfig(gw nsName, certSecret string) ([]byte, error) {
	level := c.Proxy.LogLevel
	if level == "" {
		level = "info"
	}

	k8s := map[string]any{
		"enabled":            true,
		"mode":               ModeProxy,
		"controller_name":    c.controllerName(),
		"namespaces":         c.Namespaces,
		"gateways":           []string{gw.String()},
		"certificate_secret": certSecret,
		"port_offset":        c.PortOffset,
		"health_address":     ":" + strconv.Itoa(c.Proxy.healthPort()),
		"backend": map[string]any{
			"retry":               c.Backend.Retry,
			"strategy":            c.Backend.Strategy,
			"use_service_ip":      c.Backend.UseServiceIP,
			"allow_external_name": c.Backend.AllowExternalName,
		},
	}

	// set only when used, so existing proxies keep their config hash
	if c.GatewayClass != "" {
		k8s["gateway_class"] = c.GatewayClass
	}
	if c.NamespaceScoped {
		k8s["namespace_scoped"] = true
	}

	cfg := map[string]any{
		"log_level": level,
		"providers": map[string]any{"kubernetes": k8s},
	}

	return yaml.Marshal(cfg)
}

type listenerPort struct {
	port     int32
	protocol corev1.Protocol
}

func (lp listenerPort) name() string {
	if lp.protocol == corev1.ProtocolUDP {
		return "u" + strconv.Itoa(int(lp.port))
	}

	return "p" + strconv.Itoa(int(lp.port))
}

// portOf returns the Service port of a listener.
func portOf(l gwv1.Listener) listenerPort {
	lp := listenerPort{port: int32(l.Port), protocol: corev1.ProtocolTCP}
	if l.Protocol == gwv1.UDPProtocolType {
		lp.protocol = corev1.ProtocolUDP
	}

	return lp
}

// listenerPorts returns unique listener ports and protocols of the gateway
// and the given ports of its ListenerSets, skipping ports that are out of
// range with the port offset or that use the reserved health port.
func listenerPorts(gw *gwv1.Gateway, offset, reserved int, extra ...listenerPort) []listenerPort {
	var ports []listenerPort

	all := make([]listenerPort, 0, len(gw.Spec.Listeners)+len(extra))
	for _, l := range gw.Spec.Listeners {
		all = append(all, portOf(l))
	}

	all = append(all, extra...)

	for _, lp := range all {
		if local := int(lp.port) + offset; local < 1 || local > 65535 || local == reserved {
			continue
		}

		if !slices.Contains(ports, lp) {
			ports = append(ports, lp)
		}
	}

	slices.SortFunc(ports, func(a, b listenerPort) int {
		if a.port != b.port {
			return int(a.port - b.port)
		}

		return strings.Compare(string(a.protocol), string(b.protocol))
	})

	return ports
}

func (p *Provider) proxyNamespace() string {
	if p.cfg.Proxy.Namespace != "" {
		return p.cfg.Proxy.Namespace
	}

	return podNamespace()
}

// proxyNamespaceFor returns the namespace of the proxy resources of gw.
func (p *Provider) proxyNamespaceFor(gw nsName) string {
	if p.cfg.Proxy.GatewayNamespace {
		return gw.Namespace
	}

	return p.proxyNamespace()
}

// managedNamespace is the namespace listed for managed proxy resources, all
// namespaces when proxies live next to their gateways.
func (p *Provider) managedNamespace() string {
	if p.cfg.Proxy.GatewayNamespace {
		return metav1.NamespaceAll
	}

	return p.proxyNamespace()
}

// proxyServiceAccount is the ServiceAccount of the proxy pods named name.
func (p *Provider) proxyServiceAccount(name string) string {
	if p.cfg.Proxy.ownIdentity() {
		return name
	}

	return p.cfg.Proxy.ServiceAccount
}

// infrastructureLabels are the labels of the proxy resources of gw: the
// ownership labels and the labels of spec.infrastructure, which cannot
// replace ownership labels.
func infrastructureLabels(gw *gwv1.Gateway, own map[string]string) map[string]string {
	out := maps.Clone(own)

	if gw.Spec.Infrastructure != nil {
		for k, v := range gw.Spec.Infrastructure.Labels {
			if _, managed := own[string(k)]; !managed && !strings.HasPrefix(string(k), turnaPrefix) {
				out[string(k)] = string(v)
			}
		}
	}

	return out
}

// infrastructureAnnotations are the annotations of spec.infrastructure,
// with annotationManaged listing them so they can be removed later.
func infrastructureAnnotations(gw *gwv1.Gateway) map[string]string {
	if gw.Spec.Infrastructure == nil || len(gw.Spec.Infrastructure.Annotations) == 0 {
		return nil
	}

	out := make(map[string]string, len(gw.Spec.Infrastructure.Annotations)+1)
	keys := make([]string, 0, len(gw.Spec.Infrastructure.Annotations))

	for k, v := range gw.Spec.Infrastructure.Annotations {
		// annotations of turna track its own state and are not set by
		// gateway owners
		if strings.HasPrefix(string(k), turnaPrefix) {
			continue
		}

		out[string(k)] = string(v)
		keys = append(keys, string(k))
	}

	if len(keys) == 0 {
		return nil
	}

	slices.Sort(keys)
	out[annotationManaged] = strings.Join(keys, ",")

	return out
}

// withManaged sets annotationManaged to the keys of annotations, so they
// are removed when they are no longer wanted.
func withManaged(annotations map[string]string) map[string]string {
	delete(annotations, annotationManaged)

	if len(annotations) == 0 {
		return nil
	}

	keys := slices.Sorted(maps.Keys(annotations))
	annotations[annotationManaged] = strings.Join(keys, ",")

	return annotations
}

// provisionError keeps the failures of each gateway apart from cleanup
// failures, so a failing gateway does not mark the others as not programmed.
type provisionError struct {
	gateways map[nsName]error
	err      error
}

func (e *provisionError) Error() string { return e.err.Error() }

func (e *provisionError) Unwrap() error { return e.err }

// gatewayError returns the part of err that belongs to gw; errors that are
// not a provisionError apply to every gateway.
func gatewayError(err error, gw nsName) error {
	var pe *provisionError
	if errors.As(err, &pe) {
		return pe.gateways[gw]
	}

	return err
}

// provision creates or updates the proxy resources of the served gateways
// and removes resources of deleted gateways. Current objects are read from
// the informer cache when available, so unchanged gateways cost no requests.
// policies lists the BackendTLSPolicies applied per gateway.
func (p *Provider) provision(ctx context.Context, gateways []*gwv1.Gateway, snap *Snapshot, policies map[nsName]map[nsName]struct{}, setPorts ...map[nsName][]listenerPort) error {
	want := make(map[nsName]struct{}, len(gateways))
	// bundle Secrets and the Roles reading them live in the controller
	// namespace
	wantBundle := make(map[nsName]struct{}, len(gateways))
	perGateway := make(map[nsName]error)
	var errs []error

	if snap == nil {
		snap = &Snapshot{}
	}
	idx := newIndex(snap)

	for _, gw := range gateways {
		// rejected gateways are not accepted, see finishGateways
		if _, _, _, rejected := gatewayRejection(gw); rejected {
			continue
		}

		k := key(gw)
		ns, name := p.proxyNamespaceFor(k), proxyName(k)
		want[nsName{Namespace: ns, Name: name}] = struct{}{}
		wantBundle[nsName{Namespace: p.proxyNamespace(), Name: name}] = struct{}{}

		cas := bundleCACertificates(snap.BackendTLSPolicies, policies[k], idx)
		maps.Copy(cas, bundleClientCACertificates(gw, idx))

		var ports []listenerPort
		if len(setPorts) > 0 {
			ports = setPorts[0][k]
		}

		if err := p.applyProxy(ctx, ns, name, gw, idx, cas, ports...); err != nil {
			err = fmt.Errorf("provision gateway %s: %w", k.String(), err)
			perGateway[k] = err
			errs = append(errs, err)
		}
	}

	ns := p.managedNamespace()
	selector := labels.SelectorFromSet(map[string]string{labelManagedBy: managedByValue, labelController: p.controllerID()})
	core, apps, policy, rbac := p.kube.CoreV1(), p.kube.AppsV1(), p.kube.PolicyV1(), p.kube.RbacV1()

	// Each kind is cleaned up independently so a failed deletion is retried
	// even after its Deployment has already disappeared.
	errs = append(errs, cleanup(ctx, "deployment", want, p.listDeployments(ctx, ns, selector), func(k nsName, opts metav1.DeleteOptions) error {
		propagation := metav1.DeletePropagationForeground
		opts.PropagationPolicy = &propagation

		return apps.Deployments(k.Namespace).Delete(ctx, k.Name, opts)
	})...)
	errs = append(errs, cleanup(ctx, "pdb", want, p.listPDBs(ctx, ns, selector), func(k nsName, opts metav1.DeleteOptions) error {
		return policy.PodDisruptionBudgets(k.Namespace).Delete(ctx, k.Name, opts)
	})...)
	errs = append(errs, cleanup(ctx, "service", want, p.listServices(ctx, ns, selector), func(k nsName, opts metav1.DeleteOptions) error {
		return core.Services(k.Namespace).Delete(ctx, k.Name, opts)
	})...)
	errs = append(errs, cleanup(ctx, "configmap", want, p.listConfigMaps(ctx, ns, selector), func(k nsName, opts metav1.DeleteOptions) error {
		return core.ConfigMaps(k.Namespace).Delete(ctx, k.Name, opts)
	})...)
	errs = append(errs, cleanup(ctx, "secret", wantBundle, p.listSecrets(ctx, ns, selector), func(k nsName, opts metav1.DeleteOptions) error {
		return core.Secrets(k.Namespace).Delete(ctx, k.Name, opts)
	})...)

	// Bindings are cluster scoped: switching back from gateway_namespace
	// also removes the bindings of the proxies left in gateway namespaces,
	// so they lose their read access.
	if p.cfg.Proxy.ownIdentity() {
		errs = append(errs, cleanup(ctx, "serviceaccount", want, p.listServiceAccounts(ctx, ns, selector), func(k nsName, opts metav1.DeleteOptions) error {
			return core.ServiceAccounts(k.Namespace).Delete(ctx, k.Name, opts)
		})...)
		errs = append(errs, cleanup(ctx, "role", wantBundle, p.listRoles(ctx, p.proxyNamespace(), selector), func(k nsName, opts metav1.DeleteOptions) error {
			return rbac.Roles(k.Namespace).Delete(ctx, k.Name, opts)
		})...)
		errs = append(errs, cleanup(ctx, "rolebinding", wantBundle, p.listRoleBindings(ctx, p.proxyNamespace(), selector), func(k nsName, opts metav1.DeleteOptions) error {
			return rbac.RoleBindings(k.Namespace).Delete(ctx, k.Name, opts)
		})...)
		errs = append(errs, cleanupClusterRoleBindings(ctx, want, p.listClusterRoleBindings(ctx, selector), func(name string, opts metav1.DeleteOptions) error {
			return rbac.ClusterRoleBindings().Delete(ctx, name, opts)
		})...)
	}

	if len(errs) == 0 {
		return nil
	}

	return &provisionError{gateways: perGateway, err: errors.Join(errs...)}
}

type listed[T metav1.Object] struct {
	items []T
	err   error
}

func fromList[T any, P interface {
	*T
	metav1.Object
}](items []T, err error) listed[P] {
	out := listed[P]{err: err}
	for i := range items {
		out.items = append(out.items, P(&items[i]))
	}

	return out
}

// cleanup deletes listed objects that are not wanted. The list is already
// filtered by the ownership labels of this controller, the UID precondition
// protects objects recreated since the list.
func cleanup[T metav1.Object](_ context.Context, kind string, want map[nsName]struct{}, l listed[T], del func(nsName, metav1.DeleteOptions) error) []error {
	if l.err != nil {
		return []error{fmt.Errorf("list proxy %ss: %w", kind, l.err)}
	}

	var errs []error

	for _, obj := range l.items {
		k := nsName{Namespace: obj.GetNamespace(), Name: obj.GetName()}
		if _, keep := want[k]; keep {
			continue
		}

		if kind == "deployment" {
			slog.Info("kubernetes provider: removing gateway proxy", "name", k.String())
		}

		uid := obj.GetUID()
		err := del(k, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}})
		if err != nil && !apierrors.IsNotFound(err) && !apierrors.IsConflict(err) {
			errs = append(errs, fmt.Errorf("delete %s %s: %w", kind, k.String(), err))
		}
	}

	return errs
}

// cleanupClusterRoleBindings deletes the bindings of proxies that are not
// wanted; a binding is named after the proxy namespace and name.
func cleanupClusterRoleBindings(ctx context.Context, want map[nsName]struct{}, l listed[*rbacv1.ClusterRoleBinding], del func(string, metav1.DeleteOptions) error) []error {
	keep := make(map[nsName]struct{}, len(want))
	for k := range want {
		keep[nsName{Name: clusterRoleBindingName(k)}] = struct{}{}
	}

	return cleanup(ctx, "clusterrolebinding", keep, l, func(k nsName, opts metav1.DeleteOptions) error {
		return del(k.Name, opts)
	})
}

func (p *Provider) listDeployments(ctx context.Context, ns string, sel labels.Selector) listed[*appsv1.Deployment] {
	if p.proxies() != nil {
		items, err := p.proxies().deployments.Deployments(ns).List(sel)
		return listed[*appsv1.Deployment]{items: items, err: err}
	}

	l, err := p.kube.AppsV1().Deployments(ns).List(ctx, metav1.ListOptions{LabelSelector: sel.String()})
	if err != nil {
		return listed[*appsv1.Deployment]{err: err}
	}

	return fromList[appsv1.Deployment, *appsv1.Deployment](l.Items, nil)
}

func (p *Provider) listServices(ctx context.Context, ns string, sel labels.Selector) listed[*corev1.Service] {
	if p.proxies() != nil {
		items, err := p.proxies().services.Services(ns).List(sel)
		return listed[*corev1.Service]{items: items, err: err}
	}

	l, err := p.kube.CoreV1().Services(ns).List(ctx, metav1.ListOptions{LabelSelector: sel.String()})
	if err != nil {
		return listed[*corev1.Service]{err: err}
	}

	return fromList[corev1.Service, *corev1.Service](l.Items, nil)
}

func (p *Provider) listConfigMaps(ctx context.Context, ns string, sel labels.Selector) listed[*corev1.ConfigMap] {
	if p.proxies() != nil {
		items, err := p.proxies().configmaps.ConfigMaps(ns).List(sel)
		return listed[*corev1.ConfigMap]{items: items, err: err}
	}

	l, err := p.kube.CoreV1().ConfigMaps(ns).List(ctx, metav1.ListOptions{LabelSelector: sel.String()})
	if err != nil {
		return listed[*corev1.ConfigMap]{err: err}
	}

	return fromList[corev1.ConfigMap, *corev1.ConfigMap](l.Items, nil)
}

func (p *Provider) listSecrets(ctx context.Context, ns string, sel labels.Selector) listed[*corev1.Secret] {
	if p.proxies() != nil {
		items, err := p.proxies().secrets.Secrets(ns).List(sel)
		return listed[*corev1.Secret]{items: items, err: err}
	}

	l, err := p.kube.CoreV1().Secrets(ns).List(ctx, metav1.ListOptions{LabelSelector: sel.String()})
	if err != nil {
		return listed[*corev1.Secret]{err: err}
	}

	return fromList[corev1.Secret, *corev1.Secret](l.Items, nil)
}

func (p *Provider) listPDBs(ctx context.Context, ns string, sel labels.Selector) listed[*policyv1.PodDisruptionBudget] {
	if p.proxies() != nil {
		items, err := p.proxies().pdbs.PodDisruptionBudgets(ns).List(sel)
		return listed[*policyv1.PodDisruptionBudget]{items: items, err: err}
	}

	l, err := p.kube.PolicyV1().PodDisruptionBudgets(ns).List(ctx, metav1.ListOptions{LabelSelector: sel.String()})
	if err != nil {
		return listed[*policyv1.PodDisruptionBudget]{err: err}
	}

	return fromList[policyv1.PodDisruptionBudget, *policyv1.PodDisruptionBudget](l.Items, nil)
}

// cached returns a cache getter, or nil without informers.
func cached[T any](ok bool, get func(string) (T, error)) func(string) (T, error) {
	if !ok {
		return nil
	}

	return get
}

func (p *Provider) applyProxy(ctx context.Context, ns, name string, gw *gwv1.Gateway, idx *index, cas map[string][]byte, setPorts ...listenerPort) error {
	if p.proxyImage() == "" {
		return errors.New("proxy image is not set")
	}

	if idx == nil {
		idx = newIndex(&Snapshot{})
	}

	k := key(gw)
	lbls := proxyLabels(k)
	lbls[labelController] = p.controllerID()
	pl := p.proxies()

	// spec.infrastructure metadata is added to the generated resources the
	// gateway owner sees; the ownership labels win
	meta := infrastructureLabels(gw, lbls)
	annotations := infrastructureAnnotations(gw)

	// The bundle stays in the controller namespace: it holds private keys of
	// other namespaces, which gateway owners must not read.
	bundleNS := p.proxyNamespace()

	if p.cfg.Proxy.ownIdentity() {
		if err := p.applyProxyIdentity(ctx, ns, bundleNS, name, lbls, meta, annotations); err != nil {
			return err
		}
	}

	// The proxy reads only the certificates of its gateway from this Secret
	// and needs no access to Secrets of the watched namespaces.
	bundle := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: bundleNS, Labels: lbls},
		Type:       corev1.SecretTypeOpaque,
		Data:       bundleCertificates(gw, idx),
	}
	maps.Copy(bundle.Data, cas)

	if err := upsert(ctx, p.kube.CoreV1().Secrets(bundleNS), cached(pl != nil, func(n string) (*corev1.Secret, error) {
		return pl.secrets.Secrets(bundleNS).Get(n)
	}), bundle, func(cur *corev1.Secret) bool {
		if equality.Semantic.DeepEqual(cur.Data, bundle.Data) && maps.Equal(cur.Labels, lbls) && cur.Type == bundle.Type {
			return false
		}

		cur.Data = bundle.Data
		cur.StringData = nil
		cur.Labels = lbls

		return true
	}); err != nil {
		return fmt.Errorf("secret: %w", err)
	}

	config, err := p.cfg.proxyConfig(k, bundleNS+"/"+name)
	if err != nil {
		return err
	}

	sum := sha256.Sum256(config)
	hash := hex.EncodeToString(sum[:])[:16]

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: lbls},
		Data:       map[string]string{"turna.yaml": string(config)},
	}

	if err := upsert(ctx, p.kube.CoreV1().ConfigMaps(ns), cached(pl != nil, func(n string) (*corev1.ConfigMap, error) {
		return pl.configmaps.ConfigMaps(ns).Get(n)
	}), cm, func(cur *corev1.ConfigMap) bool {
		if cur.Data["turna.yaml"] == cm.Data["turna.yaml"] && maps.Equal(cur.Labels, lbls) {
			return false
		}

		cur.Data = cm.Data
		cur.Labels = lbls

		return true
	}); err != nil {
		return fmt.Errorf("configmap: %w", err)
	}

	ports := listenerPorts(gw, p.cfg.PortOffset, p.cfg.Proxy.healthPort(), setPorts...)

	svc := p.proxyService(ns, name, meta, gw, ports)
	if err := upsert(ctx, p.kube.CoreV1().Services(ns), cached(pl != nil, func(n string) (*corev1.Service, error) {
		return pl.services.Services(ns).Get(n)
	}), svc, func(cur *corev1.Service) bool {
		annotations := mergeAnnotations(cur.Annotations, svc.Annotations)

		// only an address requested from spec.addresses is replaced or
		// removed, one set by others is kept
		lbIP := cur.Spec.LoadBalancerIP
		if svc.Spec.LoadBalancerIP != "" || (lbIP != "" && lbIP == cur.Annotations[annotationLoadBalancerIP]) {
			lbIP = svc.Spec.LoadBalancerIP
		}

		if cur.Spec.Type == svc.Spec.Type && maps.Equal(cur.Spec.Selector, svc.Spec.Selector) && servicePortsEqual(cur.Spec.Ports, svc.Spec.Ports) &&
			maps.Equal(cur.Labels, meta) && maps.Equal(cur.Annotations, annotations) && cur.Spec.LoadBalancerIP == lbIP {
			return false
		}

		cur.Spec.Type = svc.Spec.Type
		cur.Spec.Selector = svc.Spec.Selector
		cur.Spec.Ports = mergeNodePorts(cur.Spec.Ports, svc.Spec.Ports)
		cur.Spec.LoadBalancerIP = lbIP
		if svc.Spec.Type != corev1.ServiceTypeLoadBalancer {
			cur.Spec.HealthCheckNodePort = 0
			cur.Spec.AllocateLoadBalancerNodePorts = nil
			cur.Spec.LoadBalancerClass = nil
			cur.Spec.LoadBalancerIP = ""
			cur.Spec.LoadBalancerSourceRanges = nil
		}
		if svc.Spec.Type == corev1.ServiceTypeClusterIP {
			for i := range cur.Spec.Ports {
				cur.Spec.Ports[i].NodePort = 0
			}
			if len(cur.Spec.ExternalIPs) == 0 {
				cur.Spec.ExternalTrafficPolicy = ""
			}
		}
		cur.Labels = meta
		cur.Annotations = annotations

		return true
	}); err != nil {
		return fmt.Errorf("service: %w", err)
	}

	dep := p.proxyDeployment(ns, name, lbls, gw, ports, hash)
	if err := upsert(ctx, p.kube.AppsV1().Deployments(ns), cached(pl != nil, func(n string) (*appsv1.Deployment, error) {
		return pl.deployments.Deployments(ns).Get(n)
	}), dep, func(cur *appsv1.Deployment) bool {
		// keeps annotations of others, e.g. kubectl rollout restart
		podAnnotations := mergeAnnotations(cur.Spec.Template.Annotations, dep.Spec.Template.Annotations)

		if maps.Equal(cur.Labels, lbls) && maps.Equal(cur.Spec.Template.Labels, dep.Spec.Template.Labels) &&
			maps.Equal(cur.Spec.Template.Annotations, podAnnotations) &&
			ptrOr(cur.Spec.Replicas, 1) == ptrOr(dep.Spec.Replicas, 1) &&
			podSpecEqual(cur.Spec.Template.Spec, dep.Spec.Template.Spec) {
			return false
		}

		cur.Labels = lbls
		cur.Spec.Replicas = dep.Spec.Replicas
		cur.Spec.Template.Labels = dep.Spec.Template.Labels
		cur.Spec.Template.Annotations = podAnnotations
		cur.Spec.Template.Spec = dep.Spec.Template.Spec

		return true
	}); err != nil {
		return fmt.Errorf("deployment: %w", err)
	}

	return p.applyPDB(ctx, ns, name, lbls)
}

// applyPDB keeps one proxy replica serving during voluntary disruptions such
// as node drains. A single replica gets no budget, it would block drains.
func (p *Provider) applyPDB(ctx context.Context, ns, name string, lbls map[string]string) error {
	pdbs := p.kube.PolicyV1().PodDisruptionBudgets(ns)
	pl := p.proxies()

	if p.cfg.Proxy.replicas() <= 1 {
		var obj *policyv1.PodDisruptionBudget
		var err error
		if pl != nil {
			obj, err = pl.pdbs.PodDisruptionBudgets(ns).Get(name)
		} else {
			obj, err = pdbs.Get(ctx, name, metav1.GetOptions{})
		}
		if apierrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("pdb: %w", err)
		}
		if !owned(obj, lbls) {
			return nil
		}

		uid := obj.GetUID()
		err = pdbs.Delete(ctx, name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}})
		if err != nil && !apierrors.IsNotFound(err) && !apierrors.IsConflict(err) {
			return fmt.Errorf("pdb: %w", err)
		}

		return nil
	}

	maxUnavailable := intstr.FromInt32(1)
	pdb := &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: lbls},
		Spec: policyv1.PodDisruptionBudgetSpec{
			MaxUnavailable: &maxUnavailable,
			Selector:       &metav1.LabelSelector{MatchLabels: map[string]string{"app.kubernetes.io/instance": lbls["app.kubernetes.io/instance"]}},
		},
	}

	if err := upsert(ctx, pdbs, cached(pl != nil, func(n string) (*policyv1.PodDisruptionBudget, error) {
		return pl.pdbs.PodDisruptionBudgets(ns).Get(n)
	}), pdb, func(cur *policyv1.PodDisruptionBudget) bool {
		if maps.Equal(cur.Labels, lbls) && cur.Spec.MinAvailable == nil &&
			equality.Semantic.DeepEqual(cur.Spec.MaxUnavailable, pdb.Spec.MaxUnavailable) &&
			equality.Semantic.DeepEqual(cur.Spec.Selector, pdb.Spec.Selector) {
			return false
		}

		cur.Labels = lbls
		cur.Spec.MinAvailable = nil
		cur.Spec.MaxUnavailable = pdb.Spec.MaxUnavailable
		cur.Spec.Selector = pdb.Spec.Selector

		return true
	}); err != nil {
		return fmt.Errorf("pdb: %w", err)
	}

	return nil
}

// bundleCertificates returns the certificate Secrets the listeners of gw,
// of its attached ListenerSets and its backend client certificate may
// reference, keyed "<namespace>_<name>_tls.crt" and "..._tls.key". Namespace
// and Secret names cannot contain '_'.
func bundleCertificates(gw *gwv1.Gateway, idx *index) map[string][]byte {
	out := map[string][]byte{}

	addFrom := func(kind, from string, ref gwv1.SecretObjectReference) {
		if ptrOr(ref.Group, "") != "" || ptrOr(ref.Kind, kindSecret) != kindSecret {
			return
		}

		ns := string(ptrOr(ref.Namespace, gwv1.Namespace(from)))
		if !idx.referenceAllowed(groupGateway, kind, from, "", kindSecret, ns, string(ref.Name)) {
			return
		}

		s, ok := idx.secrets[nsName{Namespace: ns, Name: string(ref.Name)}]
		if !ok {
			return
		}

		prefix := ns + "_" + string(ref.Name) + "_"
		out[prefix+corev1.TLSCertKey] = s.Data[corev1.TLSCertKey]
		out[prefix+corev1.TLSPrivateKeyKey] = s.Data[corev1.TLSPrivateKeyKey]
	}

	add := func(ref gwv1.SecretObjectReference) { addFrom(kindGateway, gw.Namespace, ref) }

	for _, l := range gw.Spec.Listeners {
		if l.TLS == nil {
			continue
		}

		for _, ref := range l.TLS.CertificateRefs {
			add(ref)
		}
	}

	for _, ls := range attachedListenerSets(gw, idx) {
		for _, l := range ls.Spec.Listeners {
			if l.TLS == nil {
				continue
			}

			for _, ref := range l.TLS.CertificateRefs {
				addFrom(kindListenerSet, ls.Namespace, ref)
			}
		}
	}

	if ref := backendClientCertRef(gw); ref != nil {
		add(*ref)
	}

	return out
}

// caBundlePrefix marks CA certificates of BackendTLSPolicies and of client
// certificate validation in a bundle, keyed "ca.crt-<namespace>_<name>".
// Certificate keys start with a namespace, which cannot contain '.', and
// names cannot contain '_'.
const caBundlePrefix = caCertificateKey + "-"

// bundleCACertificates returns the CA certificates referenced by the given
// BackendTLSPolicies. Proxies translate the policies themselves and only
// lack access to ConfigMaps; a proxy gets the policies applied to the
// backends of its own gateway.
func bundleCACertificates(policies []*gwv1.BackendTLSPolicy, used map[nsName]struct{}, idx *index) map[string][]byte {
	out := map[string][]byte{}

	for _, p := range policies {
		if _, ok := used[key(p)]; !ok {
			continue
		}

		for _, ref := range p.Spec.Validation.CACertificateRefs {
			if ref.Group != "" || ref.Kind != kindConfigMap {
				continue
			}

			cm, ok := idx.configMaps[nsName{Namespace: p.Namespace, Name: string(ref.Name)}]
			if !ok {
				continue
			}

			out[caBundlePrefix+p.Namespace+"_"+string(ref.Name)] = []byte(cm.Data[caCertificateKey])
		}
	}

	return out
}

// bundleClientCACertificates returns the CA certificates of the client
// certificate validation of gw that it may reference.
func bundleClientCACertificates(gw *gwv1.Gateway, idx *index) map[string][]byte {
	out := map[string][]byte{}

	if gw.Spec.TLS == nil || gw.Spec.TLS.Frontend == nil {
		return out
	}

	validations := []*gwv1.FrontendTLSValidation{gw.Spec.TLS.Frontend.Default.Validation}
	for _, pp := range gw.Spec.TLS.Frontend.PerPort {
		validations = append(validations, pp.TLS.Validation)
	}

	for _, v := range validations {
		if v == nil {
			continue
		}

		for _, ref := range v.CACertificateRefs {
			if ref.Group != "" || ref.Kind != kindConfigMap {
				continue
			}

			ns := string(ptrOr(ref.Namespace, gwv1.Namespace(gw.Namespace)))
			if !idx.referenceAllowed(groupGateway, kindGateway, gw.Namespace, "", kindConfigMap, ns, string(ref.Name)) {
				continue
			}

			if cm, ok := idx.configMaps[nsName{Namespace: ns, Name: string(ref.Name)}]; ok {
				out[caBundlePrefix+ns+"_"+string(ref.Name)] = []byte(cm.Data[caCertificateKey])
			}
		}
	}

	return out
}

// unbundleConfigMaps restores the CA ConfigMaps of a bundle.
func unbundleConfigMaps(bundle *corev1.Secret) []*corev1.ConfigMap {
	var out []*corev1.ConfigMap

	for k, v := range bundle.Data {
		rest, ok := strings.CutPrefix(k, caBundlePrefix)
		if !ok {
			continue
		}

		ns, name, ok := strings.Cut(rest, "_")
		if !ok {
			continue
		}

		out = append(out, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name},
			Data:       map[string]string{caCertificateKey: string(v)},
		})
	}

	return out
}

// unbundleSecrets restores the certificate Secrets of a bundle.
func unbundleSecrets(bundle *corev1.Secret) []*corev1.Secret {
	byName := map[nsName]*corev1.Secret{}

	for k, v := range bundle.Data {
		if strings.HasPrefix(k, caBundlePrefix) {
			continue
		}

		ref, field, ok := strings.Cut(k, "_")
		if !ok {
			continue
		}

		name, field, ok := strings.Cut(field, "_")
		if !ok || (field != corev1.TLSCertKey && field != corev1.TLSPrivateKeyKey) {
			continue
		}

		key := nsName{Namespace: ref, Name: name}
		s, ok := byName[key]
		if !ok {
			s = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Namespace: ref, Name: name},
				Type:       corev1.SecretTypeTLS,
				Data:       map[string][]byte{},
			}
			byName[key] = s
		}

		s.Data[field] = v
	}

	out := make([]*corev1.Secret, 0, len(byName))
	for _, s := range byName {
		out = append(out, s)
	}

	return out
}

func (p *Provider) proxyService(ns, name string, lbls map[string]string, gw *gwv1.Gateway, ports []listenerPort) *corev1.Service {
	typ := corev1.ServiceType(p.cfg.Proxy.ServiceType)
	if typ == "" {
		typ = corev1.ServiceTypeLoadBalancer
	}

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: lbls, Annotations: infrastructureAnnotations(gw)},
		Spec: corev1.ServiceSpec{
			Type:     typ,
			Selector: map[string]string{"app.kubernetes.io/instance": lbls["app.kubernetes.io/instance"]},
		},
	}

	if typ == corev1.ServiceTypeLoadBalancer {
		p.cfg.requestAddresses(svc, gw)
		svc.Annotations = withManaged(svc.Annotations)
	}

	for _, lp := range ports {
		svc.Spec.Ports = append(svc.Spec.Ports, corev1.ServicePort{
			Name:       lp.name(),
			Port:       lp.port,
			TargetPort: intstr.FromInt32(lp.port + int32(p.cfg.PortOffset)), //nolint:gosec // port range
			Protocol:   lp.protocol,
		})
	}

	// a Service needs at least one port
	if len(svc.Spec.Ports) == 0 {
		svc.Spec.Ports = []corev1.ServicePort{{Name: "none", Port: 1, TargetPort: intstr.FromInt32(1), Protocol: corev1.ProtocolTCP}}
	}

	return svc
}

func (p *Provider) proxyDeployment(ns, name string, lbls map[string]string, gw *gwv1.Gateway, ports []listenerPort, hash string) *appsv1.Deployment {
	replicas := p.cfg.Proxy.replicas()
	health := p.cfg.Proxy.healthPort()

	podLabels := infrastructureLabels(gw, lbls)

	podAnnotations := map[string]string{annotationConfigHash: hash}
	maps.Copy(podAnnotations, infrastructureAnnotations(gw))

	container := corev1.Container{
		Name:            proxyContainer,
		Image:           p.proxyImage(),
		ImagePullPolicy: corev1.PullPolicy(p.cfg.Proxy.ImagePullPolicy),
		Env: []corev1.EnvVar{
			{Name: "CONFIG_FILE", Value: "/etc/turna/turna.yaml"},
			{Name: "POD_NAMESPACE", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{APIVersion: "v1", FieldPath: "metadata.namespace"}}},
		},
		VolumeMounts: []corev1.VolumeMount{{Name: "config", MountPath: "/etc/turna", ReadOnly: true}},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler:  corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: "/ready", Port: intstr.FromInt(health)}},
			PeriodSeconds: 2,
		},
		LivenessProbe: &corev1.Probe{
			ProbeHandler:     corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: "/healthz", Port: intstr.FromInt(health)}},
			PeriodSeconds:    10,
			FailureThreshold: 3,
		},
		SecurityContext: &corev1.SecurityContext{
			AllowPrivilegeEscalation: ptr(false),
			ReadOnlyRootFilesystem:   ptr(true),
			Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
		},
		Resources: p.cfg.Proxy.resources(),
	}
	if container.ImagePullPolicy == "" {
		container.ImagePullPolicy = corev1.PullIfNotPresent
		image := container.Image[strings.LastIndex(container.Image, "/")+1:]
		if !strings.Contains(image, "@") && (!strings.Contains(image, ":") || strings.HasSuffix(image, ":latest")) {
			container.ImagePullPolicy = corev1.PullAlways
		}
	}

	for _, lp := range ports {
		container.Ports = append(container.Ports, corev1.ContainerPort{
			Name:          lp.name(),
			ContainerPort: lp.port + int32(p.cfg.PortOffset), //nolint:gosec // port range
			Protocol:      lp.protocol,
		})
	}

	container.Ports = append(container.Ports, corev1.ContainerPort{Name: "health", ContainerPort: int32(health), Protocol: corev1.ProtocolTCP}) //nolint:gosec // port range

	// Keep serving while load balancers and kube-proxy stop sending new
	// connections to the terminating pod. A zero sleep is rejected by
	// Kubernetes versions before 1.33.
	delay := p.cfg.Proxy.shutdownDelay()
	if secs := int64(delay.Round(time.Second) / time.Second); secs > 0 {
		container.Lifecycle = &corev1.Lifecycle{PreStop: &corev1.LifecycleHandler{Sleep: &corev1.SleepAction{Seconds: secs}}}
	}

	selector := map[string]string{"app.kubernetes.io/instance": lbls["app.kubernetes.io/instance"]}

	spec := corev1.PodSpec{
		ServiceAccountName: p.proxyServiceAccount(name),
		// the delay and the graceful server shutdown must fit the grace period
		TerminationGracePeriodSeconds: ptr(max(int64(30), int64(delay/time.Second)+15)),
		// spread replicas so a node or zone failure does not stop the gateway
		TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
			{MaxSkew: 1, TopologyKey: corev1.LabelHostname, WhenUnsatisfiable: corev1.ScheduleAnyway, LabelSelector: &metav1.LabelSelector{MatchLabels: selector}},
			{MaxSkew: 1, TopologyKey: corev1.LabelTopologyZone, WhenUnsatisfiable: corev1.ScheduleAnyway, LabelSelector: &metav1.LabelSelector{MatchLabels: selector}},
		},
		Containers: []corev1.Container{container},
		Volumes: []corev1.Volume{{
			Name:         "config",
			VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: name}}},
		}},
		SecurityContext: &corev1.PodSecurityContext{
			RunAsNonRoot:   ptr(true),
			RunAsUser:      ptr(int64(65532)),
			RunAsGroup:     ptr(int64(65532)),
			SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
		},
	}

	for _, s := range p.cfg.Proxy.ImagePullSecrets {
		spec.ImagePullSecrets = append(spec.ImagePullSecrets, corev1.LocalObjectReference{Name: s})
	}

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: lbls},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: selector},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      podLabels,
					Annotations: podAnnotations,
				},
				Spec: spec,
			},
		},
	}
}

func (pc Proxy) resources() corev1.ResourceRequirements {
	r := corev1.ResourceRequirements{Requests: corev1.ResourceList{}, Limits: corev1.ResourceList{}}

	set := func(list corev1.ResourceList, name corev1.ResourceName, v string) {
		if v == "" {
			return
		}

		if q, err := resource.ParseQuantity(v); err == nil {
			list[name] = q
		}
	}

	set(r.Requests, corev1.ResourceCPU, pc.CPURequest)
	set(r.Requests, corev1.ResourceMemory, pc.MemoryRequest)
	set(r.Limits, corev1.ResourceCPU, pc.CPULimit)
	set(r.Limits, corev1.ResourceMemory, pc.MemoryLimit)

	return r
}

type crud[T any] interface {
	Get(ctx context.Context, name string, opts metav1.GetOptions) (T, error)
	Create(ctx context.Context, obj T, opts metav1.CreateOptions) (T, error)
	Update(ctx context.Context, obj T, opts metav1.UpdateOptions) (T, error)
}

// upsert creates obj or updates the current object when mutate reports a
// change. cachedGet reads from the informer cache when not nil; the cached
// object is copied before mutation, and a stale copy fails with a conflict
// that is retried on the next sync.
func upsert[T interface {
	metav1.Object
	runtime.Object
}](ctx context.Context, client crud[T], cachedGet func(string) (T, error), obj T, mutate func(T) bool) error {
	var cur T
	var err error

	if cachedGet != nil {
		cur, err = cachedGet(obj.GetName())
		if err == nil {
			cur = cur.DeepCopyObject().(T) //nolint:forcetypeassert // same type
		}
	} else {
		cur, err = client.Get(ctx, obj.GetName(), metav1.GetOptions{})
	}

	if apierrors.IsNotFound(err) {
		_, err = client.Create(ctx, obj, metav1.CreateOptions{})
		if cachedGet == nil || !apierrors.IsAlreadyExists(err) {
			return err
		}

		// The cache may lag behind or not see the object at all, as it only
		// watches managed objects; read it from the API server instead.
		cur, err = client.Get(ctx, obj.GetName(), metav1.GetOptions{})
	}

	if err != nil {
		return err
	}

	if !owned(cur, obj.GetLabels()) {
		return fmt.Errorf("%s exists and is not managed by turna", obj.GetName())
	}

	if !mutate(cur) {
		return nil
	}

	_, err = client.Update(ctx, cur, metav1.UpdateOptions{})

	return err
}

// owned reports whether cur carries the ownership labels in want.
func owned(cur metav1.Object, want map[string]string) bool {
	l := cur.GetLabels()

	return l[labelManagedBy] == managedByValue && l[labelController] == want[labelController] &&
		l[labelGatewayName] == want[labelGatewayName] && l[labelGatewayNamespace] == want[labelGatewayNamespace]
}

// mergeAnnotations replaces the annotations set from the gateway
// infrastructure and keeps annotations written by others, such as cloud
// load balancer controllers.
func mergeAnnotations(cur, want map[string]string) map[string]string {
	out := maps.Clone(cur)
	if out == nil {
		out = make(map[string]string, len(want))
	}

	if managed := cur[annotationManaged]; managed != "" {
		for k := range strings.SplitSeq(managed, ",") {
			delete(out, k)
		}
	}
	delete(out, annotationManaged)

	maps.Copy(out, want)

	return out
}

func servicePortsEqual(cur, want []corev1.ServicePort) bool {
	if len(cur) != len(want) {
		return false
	}

	for i := range want {
		if cur[i].Name != want[i].Name || cur[i].Port != want[i].Port || cur[i].TargetPort != want[i].TargetPort || cur[i].Protocol != want[i].Protocol {
			return false
		}
	}

	return true
}

// mergeNodePorts keeps allocated node ports of unchanged ports.
func mergeNodePorts(cur, want []corev1.ServicePort) []corev1.ServicePort {
	out := slices.Clone(want)

	for i := range out {
		for _, c := range cur {
			if c.Port == out[i].Port && c.Protocol == out[i].Protocol {
				out[i].NodePort = c.NodePort
			}
		}
	}

	return out
}

func podSpecEqual(cur, want corev1.PodSpec) bool {
	if len(cur.Containers) != 1 || len(want.Containers) != 1 {
		return false
	}

	c, w := cur.Containers[0], want.Containers[0]

	return c.Image == w.Image && c.ImagePullPolicy == w.ImagePullPolicy && cur.ServiceAccountName == want.ServiceAccountName &&
		equality.Semantic.DeepEqual(c.Resources, w.Resources) &&
		equality.Semantic.DeepEqual(c.Env, w.Env) &&
		equality.Semantic.DeepEqual(cur.ImagePullSecrets, want.ImagePullSecrets) &&
		equality.Semantic.DeepEqual(c.SecurityContext, w.SecurityContext) &&
		equality.Semantic.DeepEqual(cur.SecurityContext, want.SecurityContext) &&
		probeEqual(c.ReadinessProbe, w.ReadinessProbe) && probeEqual(c.LivenessProbe, w.LivenessProbe) &&
		equality.Semantic.DeepEqual(c.Lifecycle, w.Lifecycle) &&
		equality.Semantic.DeepEqual(cur.TopologySpreadConstraints, want.TopologySpreadConstraints) &&
		ptrOr(cur.TerminationGracePeriodSeconds, 30) == ptrOr(want.TerminationGracePeriodSeconds, 30) &&
		slices.EqualFunc(c.Ports, w.Ports, func(a, b corev1.ContainerPort) bool {
			return a.Name == b.Name && a.ContainerPort == b.ContainerPort && a.Protocol == b.Protocol
		})
}

// Probe default values are set by the API server, not by fake clients.
func probeEqual(a, b *corev1.Probe) bool {
	normalize := func(p *corev1.Probe) *corev1.Probe {
		if p == nil {
			return nil
		}
		p = p.DeepCopy()
		if p.TimeoutSeconds == 0 {
			p.TimeoutSeconds = 1
		}
		if p.PeriodSeconds == 0 {
			p.PeriodSeconds = 10
		}
		if p.SuccessThreshold == 0 {
			p.SuccessThreshold = 1
		}
		if p.FailureThreshold == 0 {
			p.FailureThreshold = 3
		}
		if p.HTTPGet != nil {
			if p.HTTPGet.Scheme == "" {
				p.HTTPGet.Scheme = corev1.URISchemeHTTP
			}
		}
		return p
	}
	return equality.Semantic.DeepEqual(normalize(a), normalize(b))
}
