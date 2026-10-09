package gcptoken

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func genCert(t *testing.T) (certPEM, keyPEM string, cert *x509.Certificate) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test-client"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}

	kb, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}

	cert, _ = x509.ParseCertificate(der)

	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb})), cert
}

type fakeGoogle struct {
	srv       *httptest.Server
	stsCalls  atomic.Int32
	impCalls  atomic.Int32
	failSTS   atomic.Bool
	expiresAt time.Time
}

func newFakeGoogle(t *testing.T, clientCert *x509.Certificate) *fakeGoogle {
	t.Helper()

	f := &fakeGoogle{expiresAt: time.Now().Add(time.Hour)}
	pool := x509.NewCertPool()
	pool.AddCert(clientCert)

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/token", func(w http.ResponseWriter, r *http.Request) {
		f.stsCalls.Add(1)

		if f.failSTS.Load() {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}

		_ = r.ParseForm()
		if r.PostForm.Get("subject_token_type") != tokenTypeMTLS ||
			r.PostForm.Get("grant_type") != grantTypeTokenExchange ||
			!strings.HasPrefix(r.PostForm.Get("audience"), "//iam.googleapis.com/") {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}

		var certs []string
		if err := json.Unmarshal([]byte(r.PostForm.Get("subject_token")), &certs); err != nil || len(certs) != 1 {
			http.Error(w, "bad subject", http.StatusBadRequest)
			return
		}

		der, _ := base64.StdEncoding.DecodeString(certs[0])
		if len(r.TLS.PeerCertificates) == 0 || string(r.TLS.PeerCertificates[0].Raw) != string(der) {
			http.Error(w, "cert mismatch", http.StatusUnauthorized)
			return
		}

		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "sts-token", "expires_in": 3600})
	})
	mux.HandleFunc("/v1/projects/-/serviceAccounts/sa@proj.iam.gserviceaccount.com:generateAccessToken",
		func(w http.ResponseWriter, r *http.Request) {
			f.impCalls.Add(1)

			if r.Header.Get("Authorization") != "Bearer sts-token" {
				http.Error(w, "unauth", http.StatusUnauthorized)
				return
			}

			var body struct {
				Lifetime string `json:"lifetime"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)

			if body.Lifetime != "1800s" {
				http.Error(w, "bad lifetime "+body.Lifetime, http.StatusBadRequest)
				return
			}

			_ = json.NewEncoder(w).Encode(map[string]any{
				"accessToken": "ya29.final",
				"expireTime":  f.expiresAt.UTC().Format(time.RFC3339),
			})
		})

	f.srv = httptest.NewUnstartedServer(mux)
	f.srv.TLS = &tls.Config{ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: pool}
	f.srv.StartTLS()
	t.Cleanup(f.srv.Close)

	return f
}

func (f *fakeGoogle) caPEM() string {
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: f.srv.Certificate().Raw}))
}

func newMiddleware(t *testing.T, mutate func(g *GCPToken)) (*GCPToken, *fakeGoogle, http.Handler) {
	t.Helper()

	certPEM, keyPEM, cert := genCert(t)
	f := newFakeGoogle(t, cert)

	creds, _ := json.Marshal(map[string]any{
		"type":               "external_account",
		"audience":           "//iam.googleapis.com/projects/1/locations/global/workloadIdentityPools/p/providers/p",
		"subject_token_type": tokenTypeMTLS,
		"token_url":          f.srv.URL + "/v1/token",
		"service_account_impersonation_url": f.srv.URL +
			"/v1/projects/-/serviceAccounts/sa@proj.iam.gserviceaccount.com:generateAccessToken",
	})

	g := &GCPToken{
		Credentials: string(creds),
		Cert:        certPEM,
		Key:         keyPEM,
		CA:          f.caPEM(),
		Lifetime:    30 * time.Minute,
	}
	if mutate != nil {
		mutate(g)
	}

	m, err := g.Middleware()
	if err != nil {
		t.Fatal(err)
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(r.Header.Get("Authorization")))
	})

	return g, f, m(next)
}

func TestEndpointRawAndCache(t *testing.T) {
	_, f, h := newMiddleware(t, nil)

	for range 3 {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))

		if rec.Code != http.StatusOK {
			t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
		}

		var resp map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)

		if resp["access_token"] != "ya29.final" || resp["token_type"] != "Bearer" {
			t.Fatalf("unexpected response %v", resp)
		}
	}

	if f.stsCalls.Load() != 1 || f.impCalls.Load() != 1 {
		t.Fatalf("expected cached token, sts=%d imp=%d", f.stsCalls.Load(), f.impCalls.Load())
	}
}

func TestManagedKafkaFormat(t *testing.T) {
	_, _, h := newMiddleware(t, func(g *GCPToken) { g.Format = FormatManagedKafka })

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))

	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)

	parts := strings.Split(resp["access_token"].(string), ".")
	if len(parts) != 3 {
		t.Fatalf("expected 3 parts, got %v", parts)
	}

	payload, _ := base64.RawURLEncoding.DecodeString(parts[1])
	sig, _ := base64.RawURLEncoding.DecodeString(parts[2])

	if !strings.Contains(string(payload), `"sub":"sa@proj.iam.gserviceaccount.com"`) {
		t.Fatalf("bad payload %s", payload)
	}

	if string(sig) != "ya29.final" {
		t.Fatalf("bad signature part %s", sig)
	}
}

func TestClientAuth(t *testing.T) {
	_, _, h := newMiddleware(t, func(g *GCPToken) {
		g.ClientID = "console"
		g.ClientSecret = "s3cret"
	})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}

	req := httptest.NewRequest(http.MethodPost, "/",
		strings.NewReader("grant_type=client_credentials&client_id=console&client_secret=s3cret"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/", nil)
	req.SetBasicAuth("console", "s3cret")

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with basic auth, got %d", rec.Code)
	}
}

func TestInjectMode(t *testing.T) {
	_, _, h := newMiddleware(t, func(g *GCPToken) { g.Mode = ModeInject })

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Body.String() != "Bearer ya29.final" {
		t.Fatalf("unexpected injected header %q", rec.Body.String())
	}
}

func TestStaleTokenOnRefreshFailure(t *testing.T) {
	g, f, h := newMiddleware(t, nil)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))

	// move clock into the refresh window but before expiry
	g.now = func() time.Time { return f.expiresAt.Add(-time.Minute) }
	f.failSTS.Store(true)

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected stale token served, got %d", rec.Code)
	}

	// after expiry it must fail
	g.now = func() time.Time { return f.expiresAt.Add(time.Minute) }

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", rec.Code)
	}
}

func TestProxy(t *testing.T) {
	var connects atomic.Int32

	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			http.Error(w, "connect only", http.StatusMethodNotAllowed)
			return
		}

		connects.Add(1)

		upstream, err := net.Dial("tcp", r.Host)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer upstream.Close()

		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer conn.Close()

		_, _ = conn.Write([]byte("HTTP/1.1 200 Connection established\r\n\r\n"))

		go func() { _, _ = io.Copy(upstream, conn) }()
		_, _ = io.Copy(conn, upstream)
	}))
	t.Cleanup(proxy.Close)

	_, _, h := newMiddleware(t, func(g *GCPToken) { g.Proxy = proxy.URL })

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}

	if connects.Load() == 0 {
		t.Fatal("expected requests through proxy")
	}
}

func TestConfigValidation(t *testing.T) {
	cases := map[string]*GCPToken{
		"cert both":    {Audience: "a", Cert: "x", CertFile: "/x", Key: "y"},
		"cred both":    {Credentials: "{}", CredentialsFile: "/x"},
		"missing file": {Audience: "a", CertFile: "/does/not/exist", Key: "y"},
		"no audience":  {Cert: "x", Key: "y"},
		"bad type":     {Credentials: `{"type":"service_account"}`},
		"bad subject":  {Credentials: `{"subject_token_type":"urn:ietf:params:oauth:token-type:jwt","audience":"a"}`},
		"no cert":      {Audience: "a"},
		"bad format":   {Audience: "a", Cert: "x", Key: "y", Format: "nope"},
		"bad proxy":    {Audience: "a", Cert: "x", Key: "y", Proxy: "socks5://p:1080"},
	}

	for name, g := range cases {
		if _, err := g.Middleware(); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestFilesAndReload(t *testing.T) {
	g, f, h := newMiddleware(t, func(g *GCPToken) {
		dir := t.TempDir()
		write := func(name, content string) string {
			p := filepath.Join(dir, name)
			if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}

			return p
		}

		g.CredentialsFile = write("creds.json", g.Credentials)
		g.CertFile = write("cert.pem", g.Cert)
		g.KeyFile = write("key.pem", g.Key)
		g.CAFile = write("ca.pem", g.CA)
		g.Credentials, g.Cert, g.Key, g.CA = "", "", "", ""
	})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}

	// rotate to a cert the fake server does not trust: next refresh must use it and fail
	certPEM, keyPEM, _ := genCert(t)
	_ = os.WriteFile(g.CertFile, []byte(certPEM), 0o600)
	_ = os.WriteFile(g.KeyFile, []byte(keyPEM), 0o600)

	g.mu.Lock()
	g.cached = nil
	g.mu.Unlock()

	before := f.stsCalls.Load()

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected rotated cert to be used and rejected, got %d", rec.Code)
	}

	if f.stsCalls.Load() != before {
		t.Fatalf("handshake should fail before reaching handler")
	}
}
