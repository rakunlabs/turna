package http

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

// Client certificate validation set for an entrypoint applies only to that
// entrypoint and does not affect the shared configuration.
func TestEntryPointClientCertificates(t *testing.T) {
	caFile, issue := testClientPKI(t)
	_, issueOther := testClientPKI(t)

	ca, err := os.ReadFile(caFile)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	trusted := issue(now.Add(-time.Hour), now.Add(time.Hour))
	unknown := issueOther(now.Add(-time.Hour), now.Add(time.Hour))

	h := &HTTP{TLS: TLS{
		Store: map[string][]Certificate{"default": {writeCertFiles(t, "localhost")}},
		EntryPoints: map[string]EntryPointTLS{
			"mtls":     {ClientCAs: []string{string(ca)}},
			"fallback": {ClientCAs: []string{string(ca)}, ClientAuth: ClientAuthRequest},
			"unused":   {ClientCAs: []string{"not a certificate"}},
		},
	}}

	configs, err := h.buildTLSConfigs(map[string]struct{}{"mtls": {}, "fallback": {}, "plain": {}})
	if err != nil {
		t.Fatal(err)
	}

	for _, tt := range []struct {
		entrypoint string
		cert       *tls.Certificate
		ok         bool
	}{
		{"mtls", &trusted, true},
		{"mtls", &unknown, false},
		{"mtls", nil, false},
		{"fallback", &trusted, true},
		{"fallback", &unknown, true},
		{"fallback", nil, true},
		{"plain", nil, true},
		{"plain", &unknown, true},
	} {
		server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))
		server.TLS = configs.forEntrypoint(tt.entrypoint)
		server.StartTLS()

		transport := server.Client().Transport.(*http.Transport).Clone()
		transport.TLSClientConfig = transport.TLSClientConfig.Clone()
		if tt.cert != nil {
			transport.TLSClientConfig.Certificates = []tls.Certificate{*tt.cert}
		}

		res, err := (&http.Client{Transport: transport}).Get(server.URL)
		if err == nil {
			res.Body.Close()
		}

		if (err == nil) != tt.ok {
			t.Errorf("%s with certificate %v: err = %v, want ok %v", tt.entrypoint, tt.cert != nil, err, tt.ok)
		}

		server.Close()
	}

	if configs.base.ClientAuth != tls.NoClientCert {
		t.Errorf("shared config client auth = %v", configs.base.ClientAuth)
	}
}

func TestEntryPointClientCertificatesInvalid(t *testing.T) {
	for name, e := range map[string]EntryPointTLS{
		"no cas":   {},
		"bad ca":   {ClientCAs: []string{"not a certificate"}},
		"bad auth": {ClientCAs: []string{"x"}, ClientAuth: "maybe"},
	} {
		h := &HTTP{TLS: TLS{EntryPoints: map[string]EntryPointTLS{"e": e}}}
		if _, err := h.buildTLSConfigs(map[string]struct{}{"e": {}}); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
