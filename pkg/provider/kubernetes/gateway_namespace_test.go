package kubernetes

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func gatewayNamespaceProvider(kube *kubefake.Clientset) *Provider {
	return &Provider{kube: kube, cfg: Config{
		Mode:  ModeController,
		Proxy: Proxy{GatewayNamespace: true, ClusterRole: "turna-proxy-reader", Image: "example:1", Namespace: "turna-system"},
	}}
}

// With proxy.gateway_namespace the proxy, its ServiceAccount and its RBAC are
// created in the gateway namespace, and spec.infrastructure metadata is on
// every generated resource.
func TestGatewayNamespaceProvision(t *testing.T) {
	ctx := context.Background()
	kube := kubefake.NewSimpleClientset()
	p := gatewayNamespaceProvider(kube)

	gw := gatewayObj("team-a", "web", httpListener("http", 80, ""))
	gw.Spec.Infrastructure = &gwv1.GatewayInfrastructure{
		Labels:      map[gwv1.LabelKey]gwv1.LabelValue{"key2": "value2", labelManagedBy: "spoofed"},
		Annotations: map[gwv1.AnnotationKey]gwv1.AnnotationValue{"key1": "value1"},
	}

	if err := p.provision(ctx, []*gwv1.Gateway{gw}, nil, nil); err != nil {
		t.Fatal(err)
	}

	name := proxyName(key(gw))
	wantLabels := map[string]string{"key2": "value2", labelGatewayName: "web"}
	wantAnnotations := map[string]string{"key1": "value1"}

	subset := func(kind string, got, want map[string]string) {
		t.Helper()

		for k, v := range want {
			if got[k] != v {
				t.Errorf("%s %s = %q, want %q (all: %v)", kind, k, got[k], v, got)
			}
		}
	}

	sa, err := kube.CoreV1().ServiceAccounts("team-a").Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	subset("serviceaccount labels", sa.Labels, wantLabels)
	subset("serviceaccount annotations", sa.Annotations, wantAnnotations)

	if sa.Labels[labelManagedBy] != managedByValue {
		t.Errorf("infrastructure label replaced an ownership label: %v", sa.Labels)
	}

	svc, err := kube.CoreV1().Services("team-a").Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	subset("service labels", svc.Labels, wantLabels)
	subset("service annotations", svc.Annotations, wantAnnotations)

	dep, err := kube.AppsV1().Deployments("team-a").Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	subset("pod labels", dep.Spec.Template.Labels, wantLabels)
	subset("pod annotations", dep.Spec.Template.Annotations, wantAnnotations)

	if dep.Spec.Template.Spec.ServiceAccountName != name {
		t.Errorf("service account = %q, want %q", dep.Spec.Template.Spec.ServiceAccountName, name)
	}

	if _, err := kube.CoreV1().ConfigMaps("team-a").Get(ctx, name, metav1.GetOptions{}); err != nil {
		t.Errorf("configmap: %v", err)
	}

	// the bundle holds private keys of other namespaces and stays in the
	// controller namespace, where gateway owners cannot read it
	if _, err := kube.CoreV1().Secrets("team-a").Get(ctx, name, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Errorf("bundle in the gateway namespace: %v", err)
	}

	if _, err := kube.CoreV1().Secrets("turna-system").Get(ctx, name, metav1.GetOptions{}); err != nil {
		t.Errorf("bundle: %v", err)
	}

	cm, err := kube.CoreV1().ConfigMaps("team-a").Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(cm.Data["turna.yaml"], "certificate_secret: turna-system/"+name) {
		t.Errorf("proxy config = %s", cm.Data["turna.yaml"])
	}

	// the proxy reads only its own bundle
	role, err := kube.RbacV1().Roles("turna-system").Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if len(role.Rules) != 1 || role.Rules[0].Resources[0] != "secrets" || role.Rules[0].ResourceNames[0] != name {
		t.Errorf("role rules = %+v", role.Rules)
	}

	rb, err := kube.RbacV1().RoleBindings("turna-system").Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if len(rb.Subjects) != 1 || rb.Subjects[0].Namespace != "team-a" || rb.Subjects[0].Name != name {
		t.Errorf("role binding subjects = %+v", rb.Subjects)
	}

	crb, err := kube.RbacV1().ClusterRoleBindings().Get(ctx, clusterRoleBindingName(nsName{"team-a", name}), metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if crb.RoleRef.Name != "turna-proxy-reader" || len(crb.Subjects) != 1 || crb.Subjects[0].Namespace != "team-a" || crb.Subjects[0].Name != name {
		t.Errorf("cluster role binding = %+v", crb)
	}

	// no proxy in the controller namespace
	if deps, _ := kube.AppsV1().Deployments("turna-system").List(ctx, metav1.ListOptions{}); len(deps.Items) > 0 {
		t.Errorf("deployments in the controller namespace: %d", len(deps.Items))
	}

	// a deleted gateway removes everything, also the cluster scoped binding
	if err := p.provision(ctx, nil, nil, nil); err != nil {
		t.Fatal(err)
	}

	for kind, get := range map[string]func() error{
		"deployment": func() error {
			_, err := kube.AppsV1().Deployments("team-a").Get(ctx, name, metav1.GetOptions{})
			return err
		},
		"serviceaccount": func() error {
			_, err := kube.CoreV1().ServiceAccounts("team-a").Get(ctx, name, metav1.GetOptions{})
			return err
		},
		"role": func() error {
			_, err := kube.RbacV1().Roles("turna-system").Get(ctx, name, metav1.GetOptions{})
			return err
		},
		"secret": func() error {
			_, err := kube.CoreV1().Secrets("turna-system").Get(ctx, name, metav1.GetOptions{})
			return err
		},
		"rolebinding": func() error {
			_, err := kube.RbacV1().RoleBindings("turna-system").Get(ctx, name, metav1.GetOptions{})
			return err
		},
		"clusterrolebinding": func() error {
			_, err := kube.RbacV1().ClusterRoleBindings().Get(ctx, clusterRoleBindingName(nsName{"team-a", name}), metav1.GetOptions{})
			return err
		},
	} {
		if err := get(); !apierrors.IsNotFound(err) {
			t.Errorf("%s not removed: %v", kind, err)
		}
	}
}

// Gateways with the same name in different namespaces get separate proxies.
func TestGatewayNamespaceSeparatesNamespaces(t *testing.T) {
	ctx := context.Background()
	kube := kubefake.NewSimpleClientset()
	p := gatewayNamespaceProvider(kube)

	a := gatewayObj("a", "web", httpListener("http", 80, ""))
	b := gatewayObj("b", "web", httpListener("http", 80, ""))

	if err := p.provision(ctx, []*gwv1.Gateway{a, b}, nil, nil); err != nil {
		t.Fatal(err)
	}

	// removing one gateway keeps the proxy of the other
	if err := p.provision(ctx, []*gwv1.Gateway{b}, nil, nil); err != nil {
		t.Fatal(err)
	}

	if _, err := kube.AppsV1().Deployments("a").Get(ctx, proxyName(key(a)), metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Errorf("proxy of the removed gateway: %v", err)
	}

	if _, err := kube.AppsV1().Deployments("b").Get(ctx, proxyName(key(b)), metav1.GetOptions{}); err != nil {
		t.Errorf("proxy of the kept gateway: %v", err)
	}
}

// A changed cluster role recreates the binding, as roleRef is immutable.
func TestGatewayNamespaceClusterRoleChange(t *testing.T) {
	ctx := context.Background()
	kube := kubefake.NewSimpleClientset()
	p := gatewayNamespaceProvider(kube)
	gw := gatewayObj("team-a", "web", httpListener("http", 80, ""))

	if err := p.provision(ctx, []*gwv1.Gateway{gw}, nil, nil); err != nil {
		t.Fatal(err)
	}

	p.cfg.Proxy.ClusterRole = "other-reader"
	if err := p.provision(ctx, []*gwv1.Gateway{gw}, nil, nil); err != nil {
		t.Fatal(err)
	}

	crb, err := kube.RbacV1().ClusterRoleBindings().Get(ctx, clusterRoleBindingName(nsName{"team-a", proxyName(key(gw))}), metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if crb.RoleRef.Name != "other-reader" {
		t.Errorf("role ref = %+v", crb.RoleRef)
	}
}

// Resources in the gateway namespace with the proxy name that are not
// managed by turna are not adopted.
func TestGatewayNamespaceDoesNotAdopt(t *testing.T) {
	ctx := context.Background()
	gw := gatewayObj("team-a", "web", httpListener("http", 80, ""))
	name := proxyName(key(gw))

	kube := kubefake.NewSimpleClientset(&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: name}})
	p := gatewayNamespaceProvider(kube)

	if err := p.provision(ctx, []*gwv1.Gateway{gw}, nil, nil); err == nil {
		t.Fatal("foreign service account adopted")
	}

	if _, err := kube.RbacV1().ClusterRoleBindings().List(ctx, metav1.ListOptions{}); err != nil {
		t.Fatal(err)
	}
}

func TestGatewayNamespaceValidate(t *testing.T) {
	for name, tt := range map[string]struct {
		cfg Config
		ok  bool
	}{
		"ok":                {Config{Mode: ModeController, Proxy: Proxy{GatewayNamespace: true, ClusterRole: "r"}}, true},
		"no cluster role":   {Config{Mode: ModeController, Proxy: Proxy{GatewayNamespace: true}}, false},
		"namespace scoped":  {Config{Mode: ModeController, NamespaceScoped: true, Namespaces: []string{"a"}, GatewayClass: "c", Proxy: Proxy{GatewayNamespace: true, ClusterRole: "r"}}, false},
		"proxy mode ignore": {Config{Mode: ModeProxy, Proxy: Proxy{GatewayNamespace: true}}, true},
		"own identity":      {Config{Mode: ModeController, Proxy: Proxy{ClusterRole: "r"}}, true},
		"scoped identity":   {Config{Mode: ModeController, NamespaceScoped: true, Namespaces: []string{"a"}, GatewayClass: "c", Proxy: Proxy{ClusterRole: "r"}}, false},
	} {
		if err := tt.cfg.validate(); (err == nil) != tt.ok {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

// Gateway owners cannot set the labels and annotations turna tracks its
// state with.
func TestInfrastructureCannotSetTurnaMetadata(t *testing.T) {
	gw := gatewayObj("app", "gw")
	gw.Spec.Infrastructure = &gwv1.GatewayInfrastructure{
		Labels:      map[gwv1.LabelKey]gwv1.LabelValue{"turna.rakunlabs.com/other": "x", "team": "a"},
		Annotations: map[gwv1.AnnotationKey]gwv1.AnnotationValue{annotationConfigHash: "fixed", annotationLoadBalancerIP: "1.2.3.4", annotationManaged: "x"},
	}

	if a := infrastructureAnnotations(gw); a != nil {
		t.Errorf("annotations = %v", a)
	}

	l := infrastructureLabels(gw, map[string]string{})
	if _, ok := l["turna.rakunlabs.com/other"]; ok || l["team"] != "a" {
		t.Errorf("labels = %v", l)
	}
}

// With proxy.cluster_role and the default placement every proxy gets its
// own ServiceAccount in the controller namespace and a Role reading only its
// certificate Secret; an existing proxy with the shared ServiceAccount is
// switched in place.
func TestControllerPlacementProxyIdentity(t *testing.T) {
	ctx := context.Background()
	kube := kubefake.NewSimpleClientset()
	gw := gatewayObj("team-a", "web", httpListener("http", 80, ""))
	name := proxyName(key(gw))

	shared := &Provider{kube: kube, cfg: Config{
		Mode:  ModeController,
		Proxy: Proxy{ServiceAccount: "turna-proxy", Image: "example:1", Namespace: "turna-system"},
	}}
	if err := shared.provision(ctx, []*gwv1.Gateway{gw}, nil, nil); err != nil {
		t.Fatal(err)
	}

	before, err := kube.AppsV1().Deployments("turna-system").Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}

	own := &Provider{kube: kube, cfg: Config{
		Mode:  ModeController,
		Proxy: Proxy{ClusterRole: "turna-reader", Image: "example:1", Namespace: "turna-system"},
	}}
	if err := own.provision(ctx, []*gwv1.Gateway{gw}, nil, nil); err != nil {
		t.Fatal(err)
	}

	dep, err := kube.AppsV1().Deployments("turna-system").Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if dep.UID != before.UID {
		t.Error("deployment recreated")
	}
	if dep.Spec.Template.Spec.ServiceAccountName != name {
		t.Errorf("service account = %q, want %q", dep.Spec.Template.Spec.ServiceAccountName, name)
	}

	if _, err := kube.CoreV1().ServiceAccounts("turna-system").Get(ctx, name, metav1.GetOptions{}); err != nil {
		t.Fatal(err)
	}

	role, err := kube.RbacV1().Roles("turna-system").Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(role.Rules) != 1 || len(role.Rules[0].ResourceNames) != 1 || role.Rules[0].ResourceNames[0] != name {
		t.Errorf("role rules = %+v, want only the bundle Secret", role.Rules)
	}

	crb, err := kube.RbacV1().ClusterRoleBindings().Get(ctx, clusterRoleBindingName(nsName{"turna-system", name}), metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if crb.RoleRef.Name != "turna-reader" || crb.Subjects[0].Namespace != "turna-system" {
		t.Errorf("clusterrolebinding = %+v", crb)
	}

	// a deleted gateway removes the identity
	if err := own.provision(ctx, nil, nil, nil); err != nil {
		t.Fatal(err)
	}

	if _, err := kube.CoreV1().ServiceAccounts("turna-system").Get(ctx, name, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Errorf("serviceaccount after delete: %v", err)
	}
	if _, err := kube.RbacV1().ClusterRoleBindings().Get(ctx, clusterRoleBindingName(nsName{"turna-system", name}), metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Errorf("clusterrolebinding after delete: %v", err)
	}
}

// Switching from gateway_namespace back to the default placement removes the
// ClusterRoleBindings of the proxies left in the gateway namespaces.
func TestSwitchBackRemovesBindings(t *testing.T) {
	ctx := context.Background()
	kube := kubefake.NewSimpleClientset()
	gw := gatewayObj("team-a", "web", httpListener("http", 80, ""))
	name := proxyName(key(gw))

	if err := gatewayNamespaceProvider(kube).provision(ctx, []*gwv1.Gateway{gw}, nil, nil); err != nil {
		t.Fatal(err)
	}

	back := &Provider{kube: kube, cfg: Config{
		Mode:  ModeController,
		Proxy: Proxy{ClusterRole: "turna-proxy-reader", Image: "example:1", Namespace: "turna-system"},
	}}
	if err := back.provision(ctx, []*gwv1.Gateway{gw}, nil, nil); err != nil {
		t.Fatal(err)
	}

	if _, err := kube.RbacV1().ClusterRoleBindings().Get(ctx, clusterRoleBindingName(nsName{"team-a", name}), metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Errorf("binding of the old proxy: %v", err)
	}
	if _, err := kube.RbacV1().ClusterRoleBindings().Get(ctx, clusterRoleBindingName(nsName{"turna-system", name}), metav1.GetOptions{}); err != nil {
		t.Errorf("binding of the new proxy: %v", err)
	}
}
