package kubernetes

import (
	"fmt"
	"log/slog"
	"strings"

	corev1 "k8s.io/api/core/v1"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"

	shttp "github.com/rakunlabs/turna/pkg/server/http"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/service"
)

// backendClientCertRef returns spec.tls.backend.clientCertificateRef of gw.
func backendClientCertRef(gw *gwv1.Gateway) *gwv1.SecretObjectReference {
	if gw.Spec.TLS == nil || gw.Spec.TLS.Backend == nil {
		return nil
	}

	return gw.Spec.TLS.Backend.ClientCertificateRef
}

// backendClientCert resolves the client certificate gw presents to TLS
// backends. It returns nil without a reference.
func (t *translator) backendClientCert(gw *gwv1.Gateway) (*shttp.Certificate, *refError) {
	ref := backendClientCertRef(gw)
	if ref == nil {
		return nil, nil
	}

	if ptrOr(ref.Group, "") != "" || ptrOr(ref.Kind, kindSecret) != kindSecret {
		return nil, &refError{string(gwv1.GatewayReasonInvalidClientCertificateRef),
			fmt.Sprintf("clientCertificateRef %s %s is not a Secret", strings.TrimPrefix(string(ptrOr(ref.Group, ""))+"/"+string(ptrOr(ref.Kind, "")), "/"), ref.Name)}
	}

	ns := string(ptrOr(ref.Namespace, gwv1.Namespace(gw.Namespace)))
	if !t.idx.referenceAllowed(groupGateway, kindGateway, gw.Namespace, "", kindSecret, ns, string(ref.Name)) {
		return nil, &refError{string(gwv1.GatewayReasonRefNotPermitted), fmt.Sprintf("secret %s/%s is not permitted by a ReferenceGrant", ns, ref.Name)}
	}

	secret, ok := t.idx.secrets[nsName{Namespace: ns, Name: string(ref.Name)}]
	if !ok {
		return nil, &refError{string(gwv1.GatewayReasonInvalidClientCertificateRef), fmt.Sprintf("secret %s/%s not found", ns, ref.Name)}
	}

	c := shttp.Certificate{Cert: string(secret.Data[corev1.TLSCertKey]), Key: string(secret.Data[corev1.TLSPrivateKeyKey])}
	if c.Cert == "" || c.Key == "" {
		return nil, &refError{string(gwv1.GatewayReasonInvalidClientCertificateRef), fmt.Sprintf("secret %s/%s has no tls.crt/tls.key", ns, ref.Name)}
	}

	if err := validCertificate(c); err != nil {
		return nil, &refError{string(gwv1.GatewayReasonInvalidClientCertificateRef), fmt.Sprintf("secret %s/%s: %v", ns, ref.Name, err)}
	}

	return &c, nil
}

// withClientCert returns cfg with the client certificate of the gateways of
// the route being translated. A route attached to gateways with different
// certificates presents none, the backend decides.
func (t *translator) withClientCert(cfg *service.UpstreamTLS) *service.UpstreamTLS {
	var (
		cert  *shttp.Certificate
		first = true
	)

	for _, gw := range t.routeGateways {
		c := t.clientCerts[gw]

		if first {
			cert, first = c, false

			continue
		}

		if (c == nil) != (cert == nil) || (c != nil && *c != *cert) {
			slog.Warn("kubernetes provider: route gateways present different backend client certificates, none is presented",
				"gateways", fmt.Sprint(t.routeGateways))

			return cfg
		}
	}

	if cert == nil {
		return cfg
	}

	out := *cfg
	out.ClientCert, out.ClientKey = cert.Cert, cert.Key

	return &out
}
