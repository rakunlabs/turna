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

	cfg := map[string]any{
		"log_level": level,
		"providers": map[string]any{
			"kubernetes": map[string]any{
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
			},
		},
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

// listenerPorts returns unique listener ports and protocols of the gateway,
// skipping ports that are out of range with the port offset or that use the
// reserved health port.
func listenerPorts(gw *gwv1.Gateway, offset, reserved int) []listenerPort {
	var ports []listenerPort

	for _, l := range gw.Spec.Listeners {
		if local := int(l.Port) + offset; local < 1 || local > 65535 || local == reserved {
			continue
		}

		lp := listenerPort{port: int32(l.Port), protocol: corev1.ProtocolTCP}
		if l.Protocol == gwv1.UDPProtocolType {
			lp.protocol = corev1.ProtocolUDP
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
func (p *Provider) provision(ctx context.Context, gateways []*gwv1.Gateway, snap *Snapshot) error {
	ns := p.proxyNamespace()
	want := make(map[string]struct{}, len(gateways))
	perGateway := make(map[nsName]error)
	var errs []error

	if snap == nil {
		snap = &Snapshot{}
	}
	idx := newIndex(snap)

	for _, gw := range gateways {
		// gateways with spec.addresses are not accepted, see finishGateways
		if len(gw.Spec.Addresses) > 0 {
			continue
		}

		k := key(gw)
		name := proxyName(k)
		want[name] = struct{}{}

		if err := p.applyProxy(ctx, ns, name, gw, idx); err != nil {
			err = fmt.Errorf("provision gateway %s: %w", k.String(), err)
			perGateway[k] = err
			errs = append(errs, err)
		}
	}

	selector := labels.SelectorFromSet(map[string]string{labelManagedBy: managedByValue, labelController: p.controllerID()})
	core, apps, policy := p.kube.CoreV1(), p.kube.AppsV1(), p.kube.PolicyV1()

	// Each kind is cleaned up independently so a failed deletion is retried
	// even after its Deployment has already disappeared.
	errs = append(errs, cleanup(ctx, "deployment", want, p.listDeployments(ctx, ns, selector), func(name string, opts metav1.DeleteOptions) error {
		propagation := metav1.DeletePropagationForeground
		opts.PropagationPolicy = &propagation

		return apps.Deployments(ns).Delete(ctx, name, opts)
	})...)
	errs = append(errs, cleanup(ctx, "pdb", want, p.listPDBs(ctx, ns, selector), func(name string, opts metav1.DeleteOptions) error {
		return policy.PodDisruptionBudgets(ns).Delete(ctx, name, opts)
	})...)
	errs = append(errs, cleanup(ctx, "service", want, p.listServices(ctx, ns, selector), func(name string, opts metav1.DeleteOptions) error {
		return core.Services(ns).Delete(ctx, name, opts)
	})...)
	errs = append(errs, cleanup(ctx, "configmap", want, p.listConfigMaps(ctx, ns, selector), func(name string, opts metav1.DeleteOptions) error {
		return core.ConfigMaps(ns).Delete(ctx, name, opts)
	})...)
	errs = append(errs, cleanup(ctx, "secret", want, p.listSecrets(ctx, ns, selector), func(name string, opts metav1.DeleteOptions) error {
		return core.Secrets(ns).Delete(ctx, name, opts)
	})...)

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
func cleanup[T metav1.Object](_ context.Context, kind string, want map[string]struct{}, l listed[T], del func(string, metav1.DeleteOptions) error) []error {
	if l.err != nil {
		return []error{fmt.Errorf("list proxy %ss: %w", kind, l.err)}
	}

	var errs []error

	for _, obj := range l.items {
		if _, keep := want[obj.GetName()]; keep {
			continue
		}

		if kind == "deployment" {
			slog.Info("kubernetes provider: removing gateway proxy", "name", obj.GetName())
		}

		uid := obj.GetUID()
		err := del(obj.GetName(), metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}})
		if err != nil && !apierrors.IsNotFound(err) && !apierrors.IsConflict(err) {
			errs = append(errs, fmt.Errorf("delete %s %s: %w", kind, obj.GetName(), err))
		}
	}

	return errs
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

func (p *Provider) applyProxy(ctx context.Context, ns, name string, gw *gwv1.Gateway, idx *index) error {
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

	// The proxy reads only the certificates of its gateway from this Secret
	// and needs no access to Secrets of the watched namespaces.
	bundle := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: lbls},
		Type:       corev1.SecretTypeOpaque,
		Data:       bundleCertificates(gw, idx),
	}

	if err := upsert(ctx, p.kube.CoreV1().Secrets(ns), cached(pl != nil, func(n string) (*corev1.Secret, error) {
		return pl.secrets.Secrets(ns).Get(n)
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

	config, err := p.cfg.proxyConfig(k, ns+"/"+name)
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

	ports := listenerPorts(gw, p.cfg.PortOffset, p.cfg.Proxy.healthPort())

	svc := p.proxyService(ns, name, lbls, gw, ports)
	if err := upsert(ctx, p.kube.CoreV1().Services(ns), cached(pl != nil, func(n string) (*corev1.Service, error) {
		return pl.services.Services(ns).Get(n)
	}), svc, func(cur *corev1.Service) bool {
		if cur.Spec.Type == svc.Spec.Type && maps.Equal(cur.Spec.Selector, svc.Spec.Selector) && servicePortsEqual(cur.Spec.Ports, svc.Spec.Ports) &&
			maps.Equal(cur.Labels, lbls) && maps.Equal(cur.Annotations, svc.Annotations) {
			return false
		}

		cur.Spec.Type = svc.Spec.Type
		cur.Spec.Selector = svc.Spec.Selector
		cur.Spec.Ports = mergeNodePorts(cur.Spec.Ports, svc.Spec.Ports)
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
		cur.Labels = lbls
		cur.Annotations = svc.Annotations

		return true
	}); err != nil {
		return fmt.Errorf("service: %w", err)
	}

	dep := p.proxyDeployment(ns, name, lbls, gw, ports, hash)
	if err := upsert(ctx, p.kube.AppsV1().Deployments(ns), cached(pl != nil, func(n string) (*appsv1.Deployment, error) {
		return pl.deployments.Deployments(ns).Get(n)
	}), dep, func(cur *appsv1.Deployment) bool {
		if cur.Spec.Template.Annotations[annotationConfigHash] == hash &&
			maps.Equal(cur.Labels, lbls) && maps.Equal(cur.Spec.Template.Labels, dep.Spec.Template.Labels) &&
			ptrOr(cur.Spec.Replicas, 1) == ptrOr(dep.Spec.Replicas, 1) &&
			podSpecEqual(cur.Spec.Template.Spec, dep.Spec.Template.Spec) {
			return false
		}

		cur.Labels = lbls
		cur.Spec.Replicas = dep.Spec.Replicas
		cur.Spec.Template.Labels = dep.Spec.Template.Labels
		if cur.Spec.Template.Annotations == nil {
			cur.Spec.Template.Annotations = make(map[string]string)
		}
		cur.Spec.Template.Annotations[annotationConfigHash] = hash
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

// bundleCertificates returns the certificate Secrets the listeners of gw may
// reference, keyed "<namespace>_<name>_tls.crt" and "..._tls.key". Namespace
// and Secret names cannot contain '_'.
func bundleCertificates(gw *gwv1.Gateway, idx *index) map[string][]byte {
	out := map[string][]byte{}

	for _, l := range gw.Spec.Listeners {
		if l.TLS == nil {
			continue
		}

		for _, ref := range l.TLS.CertificateRefs {
			if ptrOr(ref.Group, "") != "" || ptrOr(ref.Kind, kindSecret) != kindSecret {
				continue
			}

			ns := string(ptrOr(ref.Namespace, gwv1.Namespace(gw.Namespace)))
			if !idx.referenceAllowed(groupGateway, kindGateway, gw.Namespace, "", kindSecret, ns, string(ref.Name)) {
				continue
			}

			s, ok := idx.secrets[nsName{Namespace: ns, Name: string(ref.Name)}]
			if !ok {
				continue
			}

			prefix := ns + "_" + string(ref.Name) + "_"
			out[prefix+corev1.TLSCertKey] = s.Data[corev1.TLSCertKey]
			out[prefix+corev1.TLSPrivateKeyKey] = s.Data[corev1.TLSPrivateKeyKey]
		}
	}

	return out
}

// unbundleSecrets restores the certificate Secrets of a bundle.
func unbundleSecrets(bundle *corev1.Secret) []*corev1.Secret {
	byName := map[nsName]*corev1.Secret{}

	for k, v := range bundle.Data {
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
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: lbls},
		Spec: corev1.ServiceSpec{
			Type:     typ,
			Selector: map[string]string{"app.kubernetes.io/instance": lbls["app.kubernetes.io/instance"]},
		},
	}

	if gw.Spec.Infrastructure != nil && len(gw.Spec.Infrastructure.Annotations) > 0 {
		svc.Annotations = make(map[string]string, len(gw.Spec.Infrastructure.Annotations))
		for k, v := range gw.Spec.Infrastructure.Annotations {
			svc.Annotations[string(k)] = string(v)
		}
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

	podLabels := map[string]string{}
	for k, v := range lbls {
		podLabels[k] = v
	}

	if gw.Spec.Infrastructure != nil {
		for k, v := range gw.Spec.Infrastructure.Labels {
			// Identity labels must match the immutable Deployment selector and
			// Service selector, regardless of user infrastructure metadata.
			if _, managed := lbls[string(k)]; managed {
				continue
			}
			podLabels[string(k)] = string(v)
		}
	}

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
	// connections to the terminating pod.
	delay := p.cfg.Proxy.shutdownDelay()
	if delay > 0 {
		container.Lifecycle = &corev1.Lifecycle{PreStop: &corev1.LifecycleHandler{Sleep: &corev1.SleepAction{Seconds: int64(delay.Round(time.Second) / time.Second)}}}
	}

	selector := map[string]string{"app.kubernetes.io/instance": lbls["app.kubernetes.io/instance"]}

	spec := corev1.PodSpec{
		ServiceAccountName: p.cfg.Proxy.ServiceAccount,
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
					Annotations: map[string]string{annotationConfigHash: hash},
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

		return err
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
