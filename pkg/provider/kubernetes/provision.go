package kubernetes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
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
func (c *Config) proxyConfig(gw nsName) ([]byte, error) {
	health := c.Proxy.HealthPort
	if health == 0 {
		health = 9901
	}

	level := c.Proxy.LogLevel
	if level == "" {
		level = "info"
	}

	cfg := map[string]any{
		"log_level": level,
		"providers": map[string]any{
			"kubernetes": map[string]any{
				"enabled":         true,
				"mode":            ModeProxy,
				"controller_name": c.controllerName(),
				"namespaces":      c.Namespaces,
				"gateways":        []string{gw.String()},
				"port_offset":     c.PortOffset,
				"health_address":  ":" + strconv.Itoa(health),
				"backend": map[string]any{
					"retry":          c.Backend.Retry,
					"strategy":       c.Backend.Strategy,
					"use_service_ip": c.Backend.UseServiceIP,
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
// skipping ports that are out of range with the port offset.
func listenerPorts(gw *gwv1.Gateway, offset int) []listenerPort {
	var ports []listenerPort

	for _, l := range gw.Spec.Listeners {
		if local := int(l.Port) + offset; local < 1 || local > 65535 {
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
// and removes resources of deleted gateways.
func (p *Provider) provision(ctx context.Context, gateways []*gwv1.Gateway) error {
	ns := p.proxyNamespace()
	want := make(map[string]struct{}, len(gateways))
	perGateway := make(map[nsName]error)
	var errs []error

	result := func() error {
		if len(errs) == 0 {
			return nil
		}

		return &provisionError{gateways: perGateway, err: errors.Join(errs...)}
	}

	for _, gw := range gateways {
		k := key(gw)
		name := proxyName(k)
		want[name] = struct{}{}

		if err := p.applyProxy(ctx, ns, name, gw); err != nil {
			err = fmt.Errorf("provision gateway %s: %w", k.String(), err)
			perGateway[k] = err
			errs = append(errs, err)
		}
	}

	selector := labels.SelectorFromSet(map[string]string{labelManagedBy: managedByValue, labelController: p.controllerID()}).String()

	deps, err := p.kube.AppsV1().Deployments(ns).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		errs = append(errs, fmt.Errorf("list proxies: %w", err))

		return result()
	}

	for _, d := range deps.Items {
		if _, ok := want[d.Name]; ok {
			continue
		}

		slog.Info("kubernetes provider: removing gateway proxy", "name", d.Name)

		policy := metav1.DeletePropagationForeground
		opts := metav1.DeleteOptions{PropagationPolicy: &policy, Preconditions: &metav1.Preconditions{UID: &d.UID}}
		if err := p.kube.AppsV1().Deployments(ns).Delete(ctx, d.Name, opts); err != nil && !apierrors.IsNotFound(err) {
			errs = append(errs, fmt.Errorf("delete deployment %s: %w", d.Name, err))
			continue
		}
	}
	// List each kind independently so a failed deletion is retried even after
	// its Deployment has already disappeared.
	svcs, err := p.kube.CoreV1().Services(ns).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		errs = append(errs, fmt.Errorf("list proxy services: %w", err))
	} else {
		for _, svc := range svcs.Items {
			if _, keep := want[svc.Name]; !keep {
				if err := deleteOwned(ctx, p.kube.CoreV1().Services(ns), svc.Name, p.controllerID()); err != nil {
					errs = append(errs, err)
				}
			}
		}
	}
	cms, err := p.kube.CoreV1().ConfigMaps(ns).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		errs = append(errs, fmt.Errorf("list proxy configmaps: %w", err))
	} else {
		for _, cm := range cms.Items {
			if _, keep := want[cm.Name]; !keep {
				if err := deleteOwned(ctx, p.kube.CoreV1().ConfigMaps(ns), cm.Name, p.controllerID()); err != nil {
					errs = append(errs, err)
				}
			}
		}
	}
	return result()
}

func (p *Provider) applyProxy(ctx context.Context, ns, name string, gw *gwv1.Gateway) error {
	k := key(gw)
	lbls := proxyLabels(k)
	lbls[labelController] = p.controllerID()

	config, err := p.cfg.proxyConfig(k)
	if err != nil {
		return err
	}

	sum := sha256.Sum256(config)
	hash := hex.EncodeToString(sum[:])[:16]

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: lbls},
		Data:       map[string]string{"turna.yaml": string(config)},
	}

	if err := upsert(ctx, p.kube.CoreV1().ConfigMaps(ns), cm, func(cur *corev1.ConfigMap) bool {
		if cur.Data["turna.yaml"] == cm.Data["turna.yaml"] && maps(cur.Labels, lbls) {
			return false
		}

		cur.Data = cm.Data
		cur.Labels = lbls

		return true
	}); err != nil {
		return fmt.Errorf("configmap: %w", err)
	}

	ports := listenerPorts(gw, p.cfg.PortOffset)

	svc := p.proxyService(ns, name, lbls, gw, ports)
	if err := upsert(ctx, p.kube.CoreV1().Services(ns), svc, func(cur *corev1.Service) bool {
		if cur.Spec.Type == svc.Spec.Type && maps(cur.Spec.Selector, svc.Spec.Selector) && servicePortsEqual(cur.Spec.Ports, svc.Spec.Ports) &&
			maps(cur.Labels, lbls) && maps(cur.Annotations, svc.Annotations) {
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
	if err := upsert(ctx, p.kube.AppsV1().Deployments(ns), dep, func(cur *appsv1.Deployment) bool {
		if cur.Spec.Template.Annotations[annotationConfigHash] == hash &&
			maps(cur.Labels, lbls) && maps(cur.Spec.Template.Labels, dep.Spec.Template.Labels) &&
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

	return nil
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
	replicas := p.cfg.Proxy.Replicas
	if replicas == 0 {
		replicas = 1
	}

	health := p.cfg.Proxy.HealthPort
	if health == 0 {
		health = 9901
	}

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
		Image:           p.cfg.Proxy.Image,
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

	spec := corev1.PodSpec{
		ServiceAccountName: p.cfg.Proxy.ServiceAccount,
		Containers:         []corev1.Container{container},
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
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app.kubernetes.io/instance": lbls["app.kubernetes.io/instance"]}},
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
// change.
func upsert[T metav1.Object](ctx context.Context, client crud[T], obj T, mutate func(T) bool) error {
	cur, err := client.Get(ctx, obj.GetName(), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = client.Create(ctx, obj, metav1.CreateOptions{})

		return err
	}

	if err != nil {
		return err
	}

	if cur.GetLabels()[labelManagedBy] != managedByValue || cur.GetLabels()[labelController] != obj.GetLabels()[labelController] ||
		cur.GetLabels()[labelGatewayName] != obj.GetLabels()[labelGatewayName] ||
		cur.GetLabels()[labelGatewayNamespace] != obj.GetLabels()[labelGatewayNamespace] {
		return fmt.Errorf("%s exists and is not managed by turna", obj.GetName())
	}

	if !mutate(cur) {
		return nil
	}

	_, err = client.Update(ctx, cur, metav1.UpdateOptions{})

	return err
}

func maps(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}

	for k, v := range a {
		if value, ok := b[k]; !ok || value != v {
			return false
		}
	}

	return true
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

type deletable[T metav1.Object] interface {
	Get(context.Context, string, metav1.GetOptions) (T, error)
	Delete(context.Context, string, metav1.DeleteOptions) error
}

func deleteOwned[T metav1.Object](ctx context.Context, client deletable[T], name, controller string) error {
	obj, err := client.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if obj.GetLabels()[labelManagedBy] != managedByValue || obj.GetLabels()[labelController] != controller {
		return nil
	}
	uid := obj.GetUID()
	err = client.Delete(ctx, name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}})
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}
