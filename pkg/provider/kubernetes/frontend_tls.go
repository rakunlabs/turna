package kubernetes

import (
	"fmt"
	"slices"
	"strings"

	gwv1 "sigs.k8s.io/gateway-api/apis/v1"

	shttp "github.com/rakunlabs/turna/pkg/server/http"
)

// frontendValidation returns the client certificate validation of an HTTPS
// listener port: the perPort entry of the port, otherwise the default.
func frontendValidation(gw *gwv1.Gateway, port gwv1.PortNumber) *gwv1.FrontendTLSValidation {
	if gw.Spec.TLS == nil || gw.Spec.TLS.Frontend == nil {
		return nil
	}

	f := gw.Spec.TLS.Frontend
	for _, pp := range f.PerPort {
		if pp.Port == port {
			return pp.TLS.Validation
		}
	}

	return f.Default.Validation
}

// clientValidation resolves the CA certificates of v referenced by gw. It
// returns the usable CA bundles and an error describing the first invalid
// reference; every reference must be valid for the listener to resolve.
func (t *translator) clientValidation(gw *gwv1.Gateway, v *gwv1.FrontendTLSValidation) ([]string, *refError) {
	var (
		cas      []string
		firstErr *refError
	)

	setErr := func(reason gwv1.ListenerConditionReason, format string, args ...any) {
		if firstErr == nil {
			firstErr = &refError{string(reason), fmt.Sprintf(format, args...)}
		}
	}

	for _, ref := range v.CACertificateRefs {
		if ref.Group != "" || ref.Kind != kindConfigMap {
			setErr(gwv1.ListenerReasonInvalidCACertificateKind, "caCertificateRef %s %s is not a ConfigMap", strings.TrimPrefix(string(ref.Group)+"/"+string(ref.Kind), "/"), ref.Name)

			continue
		}

		ns := string(ptrOr(ref.Namespace, gwv1.Namespace(gw.Namespace)))
		if !t.idx.referenceAllowed(groupGateway, kindGateway, gw.Namespace, "", kindConfigMap, ns, string(ref.Name)) {
			setErr(gwv1.ListenerReasonRefNotPermitted, "ConfigMap %s/%s is not permitted by a ReferenceGrant", ns, ref.Name)

			continue
		}

		cm, ok := t.idx.configMaps[nsName{Namespace: ns, Name: string(ref.Name)}]
		if !ok {
			setErr(gwv1.ListenerReasonInvalidCACertificateRef, "ConfigMap %s/%s not found", ns, ref.Name)

			continue
		}

		ca := cm.Data[caCertificateKey]
		if !validCABundle(ca) {
			setErr(gwv1.ListenerReasonInvalidCACertificateRef, "ConfigMap %s/%s has no valid PEM certificate in %s", ns, ref.Name, caCertificateKey)

			continue
		}

		cas = append(cas, ca)
	}

	return cas, firstErr
}

// insecureFrontend reports whether a listener of gw accepts clients without
// a valid certificate despite a validation.
func insecureFrontend(gw *gwv1.Gateway) bool {
	if gw.Spec.TLS == nil || gw.Spec.TLS.Frontend == nil {
		return false
	}

	f := gw.Spec.TLS.Frontend
	if f.Default.Validation != nil && f.Default.Validation.Mode == gwv1.AllowInsecureFallback {
		return true
	}

	for _, pp := range f.PerPort {
		if pp.TLS.Validation != nil && pp.TLS.Validation.Mode == gwv1.AllowInsecureFallback {
			return true
		}
	}

	return false
}

// sameClientValidation reports whether two listeners of a port ask for the
// same client certificates.
func sameClientValidation(a, b *listener) bool {
	return slices.Equal(a.clientCAs, b.clientCAs) && (a.clientCAs == nil) == (b.clientCAs == nil) && a.clientFallback == b.clientFallback
}

// entrypointTLS returns the client certificate settings of an HTTPS
// entrypoint. Listeners sharing the entrypoint must ask for the same
// validation, see mergePorts.
func entrypointTLS(li *listener) (shttp.EntryPointTLS, bool) {
	if li.clientCAs == nil {
		return shttp.EntryPointTLS{}, false
	}

	out := shttp.EntryPointTLS{ClientCAs: li.clientCAs, ClientAuth: shttp.ClientAuthRequire}
	if li.clientFallback {
		out.ClientAuth = shttp.ClientAuthRequest
	}

	return out, true
}
