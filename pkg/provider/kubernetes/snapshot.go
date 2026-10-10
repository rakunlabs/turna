package kubernetes

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// Snapshot is a consistent view of the watched resources.
type Snapshot struct {
	GatewayClasses  []*gwv1.GatewayClass
	Gateways        []*gwv1.Gateway
	HTTPRoutes      []*gwv1.HTTPRoute
	GRPCRoutes      []*gwv1.GRPCRoute
	TLSRoutes       []*gwv1.TLSRoute
	TCPRoutes       []*gwv1.TCPRoute
	UDPRoutes       []*gwv1.UDPRoute
	ReferenceGrants []*gwv1.ReferenceGrant
	Services        []*corev1.Service
	EndpointSlices  []*discoveryv1.EndpointSlice
	Secrets         []*corev1.Secret
	Namespaces      []*corev1.Namespace
}

type nsName struct {
	Namespace string
	Name      string
}

func (n nsName) String() string {
	return n.Namespace + "/" + n.Name
}

func key(o metav1.Object) nsName {
	return nsName{Namespace: o.GetNamespace(), Name: o.GetName()}
}

// index provides lookups over a snapshot.
type index struct {
	services   map[nsName]*corev1.Service
	slices     map[nsName][]*discoveryv1.EndpointSlice
	secrets    map[nsName]*corev1.Secret
	namespaces map[string]*corev1.Namespace
	grants     map[string][]*gwv1.ReferenceGrant
}

func newIndex(s *Snapshot) *index {
	idx := &index{
		services:   make(map[nsName]*corev1.Service, len(s.Services)),
		slices:     make(map[nsName][]*discoveryv1.EndpointSlice),
		secrets:    make(map[nsName]*corev1.Secret, len(s.Secrets)),
		namespaces: make(map[string]*corev1.Namespace, len(s.Namespaces)),
		grants:     make(map[string][]*gwv1.ReferenceGrant),
	}

	for _, o := range s.Services {
		idx.services[key(o)] = o
	}

	for _, o := range s.EndpointSlices {
		svc := o.Labels[discoveryv1.LabelServiceName]
		if svc == "" {
			continue
		}

		k := nsName{Namespace: o.Namespace, Name: svc}
		idx.slices[k] = append(idx.slices[k], o)
	}

	for _, o := range s.Secrets {
		idx.secrets[key(o)] = o
	}

	for _, o := range s.Namespaces {
		idx.namespaces[o.Name] = o
	}

	for _, o := range s.ReferenceGrants {
		idx.grants[o.Namespace] = append(idx.grants[o.Namespace], o)
	}

	return idx
}

// referenceAllowed reports whether an object of fromGroup/fromKind in
// fromNamespace may reference toGroup/toKind/toName in toNamespace.
func (idx *index) referenceAllowed(fromGroup, fromKind, fromNamespace, toGroup, toKind, toNamespace, toName string) bool {
	if fromNamespace == toNamespace {
		return true
	}

	for _, g := range idx.grants[toNamespace] {
		fromOK := false
		for _, f := range g.Spec.From {
			if string(f.Group) == fromGroup && string(f.Kind) == fromKind && string(f.Namespace) == fromNamespace {
				fromOK = true

				break
			}
		}

		if !fromOK {
			continue
		}

		for _, t := range g.Spec.To {
			if string(t.Group) == toGroup && string(t.Kind) == toKind && (t.Name == nil || string(*t.Name) == toName) {
				return true
			}
		}
	}

	return false
}

func (idx *index) namespaceLabels(ns string) labels.Set {
	if n, ok := idx.namespaces[ns]; ok {
		return n.Labels
	}

	return nil
}

func sanitize(parts ...string) string {
	s := strings.Join(parts, "-")
	// Hash the unmodified parts with separators that cannot occur in these
	// values. Sanitization and truncation must not erase identity or boundaries.
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	s = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			return r
		default:
			return '_'
		}
	}, s)
	if len(s) > 183 {
		s = s[:183]
	}
	return s + "-" + hex.EncodeToString(sum[:])[:16]
}

func ptrOr[T any](p *T, def T) T {
	if p == nil {
		return def
	}

	return *p
}

func ptr[T any](v T) *T {
	return &v
}
