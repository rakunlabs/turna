package kubernetes

import (
	"context"
	"fmt"
	"maps"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

// clusterRoleBindingName names the binding of the proxy ServiceAccount k to
// proxy.cluster_role; the proxy name is already unique per gateway.
func clusterRoleBindingName(k nsName) string {
	return "turna-proxy:" + k.Namespace + ":" + k.Name
}

// applyProxyIdentity creates the ServiceAccount of a proxy in the gateway
// namespace, a Role in bundleNS reading only its bundle Secret, and the
// binding to proxy.cluster_role for the watched resources.
func (p *Provider) applyProxyIdentity(ctx context.Context, ns, bundleNS, name string, own, meta map[string]string, annotations map[string]string) error {
	pl := p.proxies()
	core, rbac := p.kube.CoreV1(), p.kube.RbacV1()

	sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: meta, Annotations: annotations}}
	if err := upsert(ctx, core.ServiceAccounts(ns), cached(pl != nil && pl.serviceAccounts != nil, func(n string) (*corev1.ServiceAccount, error) {
		return pl.serviceAccounts.ServiceAccounts(ns).Get(n)
	}), sa, func(cur *corev1.ServiceAccount) bool {
		merged := mergeAnnotations(cur.Annotations, annotations)
		if maps.Equal(cur.Labels, meta) && maps.Equal(cur.Annotations, merged) {
			return false
		}

		cur.Labels = meta
		cur.Annotations = merged

		return true
	}); err != nil {
		return fmt.Errorf("serviceaccount: %w", err)
	}

	rules := []rbacv1.PolicyRule{{
		APIGroups:     []string{""},
		Resources:     []string{"secrets"},
		ResourceNames: []string{name},
		Verbs:         []string{"get", "list", "watch"},
	}}

	role := &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: bundleNS, Labels: own}, Rules: rules}
	if err := upsert(ctx, rbac.Roles(bundleNS), cached(pl != nil && pl.roles != nil, func(n string) (*rbacv1.Role, error) {
		return pl.roles.Roles(bundleNS).Get(n)
	}), role, func(cur *rbacv1.Role) bool {
		if maps.Equal(cur.Labels, own) && equality.Semantic.DeepEqual(cur.Rules, rules) {
			return false
		}

		cur.Labels = own
		cur.Rules = rules

		return true
	}); err != nil {
		return fmt.Errorf("role: %w", err)
	}

	subjects := []rbacv1.Subject{{Kind: rbacv1.ServiceAccountKind, Name: name, Namespace: ns}}

	rb := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: bundleNS, Labels: own},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: name},
		Subjects:   subjects,
	}
	if err := upsert(ctx, rbac.RoleBindings(bundleNS), cached(pl != nil && pl.roleBindings != nil, func(n string) (*rbacv1.RoleBinding, error) {
		return pl.roleBindings.RoleBindings(bundleNS).Get(n)
	}), rb, func(cur *rbacv1.RoleBinding) bool {
		// the role reference is immutable and never changes for a name
		if maps.Equal(cur.Labels, own) && equality.Semantic.DeepEqual(cur.Subjects, subjects) {
			return false
		}

		cur.Labels = own
		cur.Subjects = subjects

		return true
	}); err != nil {
		return fmt.Errorf("rolebinding: %w", err)
	}

	crbName := clusterRoleBindingName(nsName{Namespace: ns, Name: name})
	ref := rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: p.cfg.Proxy.ClusterRole}

	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: crbName, Labels: own},
		RoleRef:    ref,
		Subjects:   subjects,
	}

	getCRB := cached(pl != nil && pl.clusterRoleBindings != nil, func(n string) (*rbacv1.ClusterRoleBinding, error) {
		return pl.clusterRoleBindings.Get(n)
	})

	refChanged := false

	if err := upsert(ctx, rbac.ClusterRoleBindings(), getCRB, crb, func(cur *rbacv1.ClusterRoleBinding) bool {
		if cur.RoleRef != ref {
			// roleRef is immutable, the binding is recreated below
			refChanged = true

			return false
		}

		if maps.Equal(cur.Labels, own) && equality.Semantic.DeepEqual(cur.Subjects, subjects) {
			return false
		}

		cur.Labels = own
		cur.Subjects = subjects

		return true
	}); err != nil {
		return fmt.Errorf("clusterrolebinding: %w", err)
	}

	if refChanged {
		cur, err := rbac.ClusterRoleBindings().Get(ctx, crbName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("clusterrolebinding: %w", err)
		}

		uid := cur.GetUID()
		if err := rbac.ClusterRoleBindings().Delete(ctx, crbName, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}}); err != nil {
			return fmt.Errorf("clusterrolebinding: %w", err)
		}

		if _, err := rbac.ClusterRoleBindings().Create(ctx, crb, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("clusterrolebinding: %w", err)
		}
	}

	return nil
}

func (p *Provider) listServiceAccounts(ctx context.Context, ns string, sel labels.Selector) listed[*corev1.ServiceAccount] {
	if pl := p.proxies(); pl != nil && pl.serviceAccounts != nil {
		items, err := pl.serviceAccounts.ServiceAccounts(ns).List(sel)
		return listed[*corev1.ServiceAccount]{items: items, err: err}
	}

	l, err := p.kube.CoreV1().ServiceAccounts(ns).List(ctx, metav1.ListOptions{LabelSelector: sel.String()})
	if err != nil {
		return listed[*corev1.ServiceAccount]{err: err}
	}

	return fromList[corev1.ServiceAccount, *corev1.ServiceAccount](l.Items, nil)
}

func (p *Provider) listRoles(ctx context.Context, ns string, sel labels.Selector) listed[*rbacv1.Role] {
	if pl := p.proxies(); pl != nil && pl.roles != nil {
		items, err := pl.roles.Roles(ns).List(sel)
		return listed[*rbacv1.Role]{items: items, err: err}
	}

	l, err := p.kube.RbacV1().Roles(ns).List(ctx, metav1.ListOptions{LabelSelector: sel.String()})
	if err != nil {
		return listed[*rbacv1.Role]{err: err}
	}

	return fromList[rbacv1.Role, *rbacv1.Role](l.Items, nil)
}

func (p *Provider) listRoleBindings(ctx context.Context, ns string, sel labels.Selector) listed[*rbacv1.RoleBinding] {
	if pl := p.proxies(); pl != nil && pl.roleBindings != nil {
		items, err := pl.roleBindings.RoleBindings(ns).List(sel)
		return listed[*rbacv1.RoleBinding]{items: items, err: err}
	}

	l, err := p.kube.RbacV1().RoleBindings(ns).List(ctx, metav1.ListOptions{LabelSelector: sel.String()})
	if err != nil {
		return listed[*rbacv1.RoleBinding]{err: err}
	}

	return fromList[rbacv1.RoleBinding, *rbacv1.RoleBinding](l.Items, nil)
}

func (p *Provider) listClusterRoleBindings(ctx context.Context, sel labels.Selector) listed[*rbacv1.ClusterRoleBinding] {
	if pl := p.proxies(); pl != nil && pl.clusterRoleBindings != nil {
		items, err := pl.clusterRoleBindings.List(sel)
		return listed[*rbacv1.ClusterRoleBinding]{items: items, err: err}
	}

	l, err := p.kube.RbacV1().ClusterRoleBindings().List(ctx, metav1.ListOptions{LabelSelector: sel.String()})
	if err != nil {
		return listed[*rbacv1.ClusterRoleBinding]{err: err}
	}

	return fromList[rbacv1.ClusterRoleBinding, *rbacv1.ClusterRoleBinding](l.Items, nil)
}
