package kubernetes

import (
	"cmp"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/service"
)

const (
	kindBackendTLSPolicy = "BackendTLSPolicy"
	kindConfigMap        = "ConfigMap"

	// caCertificateKey holds the PEM CA bundle in a referenced ConfigMap.
	caCertificateKey = "ca.crt"
)

// backendTLS is the validated TLS configuration of a Service port.
type backendTLS struct {
	policy nsName
	tls    *service.UpstreamTLS
}

// policyState is the translation result of one BackendTLSPolicy.
type policyState struct {
	policy *gwv1.BackendTLSPolicy

	accepted metav1.Condition
	resolved metav1.Condition

	// targets taken by an older or more specific policy
	lost map[policyTarget]bool

	// gateways whose routes use a Service targeted by the policy; status is
	// reported per gateway ancestor, true when the policy lost a target that
	// the gateway uses
	ancestors map[nsName]bool
}

// maxPolicyAncestors is the size limit of PolicyStatus.Ancestors.
const maxPolicyAncestors = 16

// backendTLSPolicies resolves the policies targeting Services. Conflicting
// policies follow GEP-713: a policy targeting a port by sectionName wins over
// one targeting the whole Service, then the older policy, then the policy
// first in namespace/name order. A conflict is reported only to the gateways
// using the lost target; other targets of the policy still apply.
func (t *translator) backendTLSPolicies(policies []*gwv1.BackendTLSPolicy) {
	sorted := slices.Clone(policies)
	slices.SortFunc(sorted, func(a, b *gwv1.BackendTLSPolicy) int {
		if c := a.CreationTimestamp.Compare(b.CreationTimestamp.Time); c != 0 {
			return c
		}

		return cmp.Compare(key(a).String(), key(b).String())
	})

	// "namespace/service" -> section ("" for the whole Service) -> policy
	claimed := map[nsName]map[string]nsName{}

	for _, p := range sorted {
		gen := p.Generation
		st := &policyState{
			policy:    p,
			accepted:  t.condition(gen, string(gwv1.PolicyConditionAccepted), true, string(gwv1.PolicyReasonAccepted), ""),
			resolved:  t.condition(gen, string(gwv1.BackendTLSPolicyConditionResolvedRefs), true, string(gwv1.BackendTLSPolicyReasonResolvedRefs), ""),
			lost:      map[policyTarget]bool{},
			ancestors: map[nsName]bool{},
		}
		t.policies[key(p)] = st

		cfg, _ := t.policyTLS(p, st)

		var targets []policyTarget

		for _, ref := range p.Spec.TargetRefs {
			if ref.Group != "" || ref.Kind != kindService {
				continue
			}

			targets = append(targets, policyTarget{
				svc:     nsName{Namespace: p.Namespace, Name: string(ref.Name)},
				section: string(ptrOr(ref.SectionName, "")),
			})
		}

		for _, target := range targets {
			if !slices.Contains(t.policyTargets[target], key(p)) {
				t.policyTargets[target] = append(t.policyTargets[target], key(p))
			}

			sections := claimed[target.svc]
			if sections == nil {
				sections = map[string]nsName{}
				claimed[target.svc] = sections
			}

			if owner, taken := sections[target.section]; taken {
				if owner != key(p) {
					st.lost[target] = true
				}

				continue
			}

			sections[target.section] = key(p)
			// an invalid policy still claims its target and fails connections
			t.backendTLS[target] = backendTLS{policy: key(p), tls: cfg}
		}
	}
}

type policyTarget struct {
	svc     nsName
	section string
}

// policyTLS validates the policy. An invalid configuration still returns a
// configuration that fails every connection, so traffic never falls back
// to plain or unverified connections.
func (t *translator) policyTLS(p *gwv1.BackendTLSPolicy, st *policyState) (*service.UpstreamTLS, bool) {
	gen := p.Generation
	v := p.Spec.Validation

	out := &service.UpstreamTLS{ServerName: string(v.Hostname)}

	for _, san := range v.SubjectAltNames {
		switch san.Type {
		case gwv1.HostnameSubjectAltNameType:
			out.SubjectAltNames = append(out.SubjectAltNames, string(san.Hostname))
		case gwv1.URISubjectAltNameType:
			out.SubjectAltNames = append(out.SubjectAltNames, string(san.URI))
		}
	}

	invalid := func(accepted, resolved metav1.Condition) (*service.UpstreamTLS, bool) {
		st.accepted, st.resolved = accepted, resolved
		out.Invalid = true

		return out, false
	}

	if v.WellKnownCACertificates != nil {
		if *v.WellKnownCACertificates != gwv1.WellKnownCACertificatesSystem || len(v.CACertificateRefs) > 0 {
			return invalid(
				t.condition(gen, string(gwv1.PolicyConditionAccepted), false, string(gwv1.PolicyReasonInvalid),
					fmt.Sprintf("wellKnownCACertificates %q is not supported", *v.WellKnownCACertificates)),
				st.resolved)
		}

		return out, true
	}

	var (
		bundle []string
		errs   []string
		reason = gwv1.BackendTLSPolicyReasonInvalidCACertificateRef
	)

	for _, ref := range v.CACertificateRefs {
		if ref.Group != "" || ref.Kind != kindConfigMap {
			errs = append(errs, fmt.Sprintf("caCertificateRef %s/%s %s is not supported", ref.Group, ref.Kind, ref.Name))
			reason = gwv1.BackendTLSPolicyReasonInvalidKind

			continue
		}

		cm, ok := t.idx.configMaps[nsName{Namespace: p.Namespace, Name: string(ref.Name)}]
		if !ok {
			errs = append(errs, fmt.Sprintf("ConfigMap %s/%s not found", p.Namespace, ref.Name))

			continue
		}

		ca := cm.Data[caCertificateKey]
		if !validCABundle(ca) {
			errs = append(errs, fmt.Sprintf("ConfigMap %s/%s has no valid PEM certificate in %s", p.Namespace, ref.Name, caCertificateKey))

			continue
		}

		bundle = append(bundle, ca)
	}

	if len(errs) > 0 {
		st.resolved = t.condition(gen, string(gwv1.BackendTLSPolicyConditionResolvedRefs), false, string(reason), strings.Join(errs, "; "))
	}

	if len(bundle) == 0 {
		return invalid(
			t.condition(gen, string(gwv1.PolicyConditionAccepted), false, string(gwv1.BackendTLSPolicyReasonNoValidCACertificate),
				"no valid CA certificate"),
			st.resolved)
	}

	out.CA = strings.Join(bundle, "\n")

	return out, true
}

func validCABundle(pemData string) bool {
	if strings.TrimSpace(pemData) == "" {
		return false
	}

	return x509.NewCertPool().AppendCertsFromPEM([]byte(pemData))
}

// upstreamTLS returns the TLS configuration of a Service port. The gateways
// of the route become ancestors of every policy targeting the port, so
// conflicted policies report their status too.
func (t *translator) upstreamTLS(svc *corev1.Service, port *corev1.ServicePort) *service.UpstreamTLS {
	k := key(svc)
	section, whole := policyTarget{svc: k, section: port.Name}, policyTarget{svc: k}

	for _, target := range []policyTarget{section, whole} {
		for _, name := range t.policyTargets[target] {
			st := t.policies[name]
			if st == nil {
				continue
			}

			for _, gw := range t.routeGateways {
				st.ancestors[gw] = st.ancestors[gw] || st.lost[target]
			}
		}
	}

	b, ok := t.backendTLS[section]
	if !ok {
		b, ok = t.backendTLS[whole]
	}

	if !ok {
		return nil
	}

	// the CA certificates of the applied policy go into the gateway bundles
	for _, gw := range t.routeGateways {
		if t.out.gatewayPolicies[gw] == nil {
			t.out.gatewayPolicies[gw] = map[nsName]struct{}{}
		}

		t.out.gatewayPolicies[gw][b.policy] = struct{}{}
	}

	return b.tls
}

// PolicyAncestors returns the status of a BackendTLSPolicy per gateway.
func (r *Result) PolicyAncestors(k nsName) []gwv1.PolicyAncestorStatus {
	return r.Policies[k]
}

// finishPolicies builds the ancestor status of every policy; policies used
// by no served gateway get no status from this controller. The status holds
// at most maxPolicyAncestors gateways, conflicted ones first.
func (t *translator) finishPolicies() {
	for k, st := range t.policies {
		gateways := make([]nsName, 0, len(st.ancestors))
		for gw := range st.ancestors {
			gateways = append(gateways, gw)
		}

		slices.SortFunc(gateways, func(a, b nsName) int {
			if st.ancestors[a] != st.ancestors[b] {
				if st.ancestors[a] {
					return -1
				}

				return 1
			}

			return cmp.Compare(a.String(), b.String())
		})

		if len(gateways) > maxPolicyAncestors {
			slog.Warn("kubernetes provider: BackendTLSPolicy status lists only the first gateways",
				"policy", k.String(), "gateways", len(gateways), "limit", maxPolicyAncestors)

			gateways = gateways[:maxPolicyAncestors]
			slices.SortFunc(gateways, func(a, b nsName) int { return cmp.Compare(a.String(), b.String()) })
		}

		out := make([]gwv1.PolicyAncestorStatus, 0, len(gateways))
		for _, gw := range gateways {
			accepted := st.accepted
			if st.ancestors[gw] && accepted.Status == metav1.ConditionTrue {
				accepted = t.condition(st.policy.Generation, string(gwv1.PolicyConditionAccepted), false, string(gwv1.PolicyReasonConflicted),
					"another BackendTLSPolicy targets the same Service and section")
			}

			out = append(out, gwv1.PolicyAncestorStatus{
				AncestorRef: gwv1.ParentReference{
					Group:     ptr(gwv1.Group(groupGateway)),
					Kind:      ptr(gwv1.Kind(kindGateway)),
					Namespace: ptr(gwv1.Namespace(gw.Namespace)),
					Name:      gwv1.ObjectName(gw.Name),
				},
				ControllerName: gwv1.GatewayController(t.cfg.controllerName()),
				Conditions:     []metav1.Condition{accepted, st.resolved},
			})
		}

		t.out.Policies[k] = out
	}
}

// tlsID identifies a TLS configuration for middleware names.
func tlsID(c *service.UpstreamTLS) string {
	h := sha256.New()
	for _, v := range append([]string{c.CA, c.ServerName, strconv.FormatBool(c.Invalid), c.ClientCert}, c.SubjectAltNames...) {
		h.Write([]byte(v))
		h.Write([]byte{0})
	}

	return hex.EncodeToString(h.Sum(nil))[:16]
}
