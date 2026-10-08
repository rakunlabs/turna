// Package gcptoken obtains Google Cloud access tokens through Workload Identity
// Federation with X.509 client certificates (mTLS) and optional service account
// impersonation, then serves them as an OAuth2 token endpoint or injects them
// into proxied requests.
//
// Certificates and credentials are given inline (PEM / JSON content), so no
// file paths are required.
package gcptoken

import (
	"bytes"
	"context"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	grantTypeTokenExchange  = "urn:ietf:params:oauth:grant-type:token-exchange"
	tokenTypeAccessToken    = "urn:ietf:params:oauth:token-type:access_token"
	tokenTypeMTLS           = "urn:ietf:params:oauth:token-type:mtls"
	scopeCloudPlatform      = "https://www.googleapis.com/auth/cloud-platform"
	defaultTokenURL         = "https://sts.mtls.googleapis.com/v1/token"
	FormatRaw               = "raw"
	FormatManagedKafka      = "managed_kafka"
	ModeEndpoint            = "endpoint"
	ModeInject              = "inject"
	defaultLifetime         = time.Hour
	defaultRefreshBefore    = 5 * time.Minute
	defaultTimeout          = 30 * time.Second
	maxResponseBody         = 1 << 20
	impersonationURLPattern = "serviceAccounts/"
)

type GCPToken struct {
	// Credentials is the raw "external_account" JSON content (optional).
	// Fields below override values from it.
	Credentials string `cfg:"credentials" log:"-"`

	Audience                       string `cfg:"audience"`
	TokenURL                       string `cfg:"token_url"`
	ServiceAccountImpersonationURL string `cfg:"service_account_impersonation_url"`

	// Cert is the PEM client certificate; may contain the full chain (leaf first).
	Cert string `cfg:"cert"`
	// Key is the PEM private key of the client certificate.
	Key string `cfg:"key" log:"-"`
	// CA is optional PEM root CAs for the Google endpoints; system roots are used if empty.
	CA string `cfg:"ca"`

	// File alternatives of the inline fields. Inline and file forms of the same
	// field cannot be used together. Cert, key and CA files are re-read on every
	// token refresh, so rotated certificates are picked up without restart.
	CredentialsFile string `cfg:"credentials_file"`
	CertFile        string `cfg:"cert_file"`
	KeyFile         string `cfg:"key_file"`
	CAFile          string `cfg:"ca_file"`

	// Scopes requested, default cloud-platform.
	Scopes []string `cfg:"scopes"`
	// Lifetime of the impersonated token, default 1h (max 12h with org policy).
	Lifetime time.Duration `cfg:"lifetime"`
	// RefreshBefore refreshes the cached token this long before expiry, default 5m.
	RefreshBefore time.Duration `cfg:"refresh_before"`
	// Timeout for upstream calls, default 30s.
	Timeout time.Duration `cfg:"timeout"`

	// Format of the returned token: "raw" (default) or "managed_kafka"
	// (Google Managed Service for Apache Kafka OAUTHBEARER wrapper).
	Format string `cfg:"format"`
	// Mode: "endpoint" (default) responds like an OAuth2 token endpoint,
	// "inject" sets the token on the request and calls the next handler.
	Mode string `cfg:"mode"`
	// Header used in inject mode, default "Authorization" (value "Bearer <token>").
	Header string `cfg:"header"`

	// ClientID / ClientSecret protect the endpoint (basic auth or form values).
	ClientID     string `cfg:"client_id"`
	ClientSecret string `cfg:"client_secret" log:"-"`

	client *http.Client
	now    func() time.Time

	mu     sync.Mutex
	cached *Token
}

type Token struct {
	AccessToken string
	Expiry      time.Time
}

type externalAccount struct {
	Type                           string `json:"type"`
	Audience                       string `json:"audience"`
	SubjectTokenType               string `json:"subject_token_type"`
	TokenURL                       string `json:"token_url"`
	ServiceAccountImpersonationURL string `json:"service_account_impersonation_url"`
}

func readFile(path string) (string, error) {
	b, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("gcp_token: %w", err)
	}

	return string(b), nil
}

func (g *GCPToken) hasCertFiles() bool {
	return g.CertFile != "" || g.KeyFile != "" || g.CAFile != ""
}

// loadCertFiles reads cert/key/ca files into the inline fields.
// It reports whether any content changed.
func (g *GCPToken) loadCertFiles() (bool, error) {
	changed := false

	for _, f := range []struct {
		path string
		dst  *string
	}{
		{g.CertFile, &g.Cert},
		{g.KeyFile, &g.Key},
		{g.CAFile, &g.CA},
	} {
		if f.path == "" {
			continue
		}

		v, err := readFile(f.path)
		if err != nil {
			return false, err
		}

		if v != *f.dst {
			*f.dst = v
			changed = true
		}
	}

	return changed, nil
}

func (g *GCPToken) init() error {
	for _, c := range []struct {
		name         string
		inline, file string
	}{
		{"credentials", g.Credentials, g.CredentialsFile},
		{"cert", g.Cert, g.CertFile},
		{"key", g.Key, g.KeyFile},
		{"ca", g.CA, g.CAFile},
	} {
		if c.inline != "" && c.file != "" {
			return fmt.Errorf("gcp_token: %s and %s_file cannot be used together", c.name, c.name)
		}
	}

	if g.CredentialsFile != "" {
		v, err := readFile(g.CredentialsFile)
		if err != nil {
			return err
		}

		g.Credentials = v
	}

	if _, err := g.loadCertFiles(); err != nil {
		return err
	}

	if g.Credentials != "" {
		var ea externalAccount
		if err := json.Unmarshal([]byte(g.Credentials), &ea); err != nil {
			return fmt.Errorf("gcp_token: parse credentials: %w", err)
		}

		if ea.Type != "" && ea.Type != "external_account" {
			return fmt.Errorf("gcp_token: unsupported credentials type %q", ea.Type)
		}

		if ea.SubjectTokenType != "" && ea.SubjectTokenType != tokenTypeMTLS {
			return fmt.Errorf("gcp_token: unsupported subject_token_type %q, only %s", ea.SubjectTokenType, tokenTypeMTLS)
		}

		if g.Audience == "" {
			g.Audience = ea.Audience
		}

		if g.TokenURL == "" {
			g.TokenURL = ea.TokenURL
		}

		if g.ServiceAccountImpersonationURL == "" {
			g.ServiceAccountImpersonationURL = ea.ServiceAccountImpersonationURL
		}
	}

	if g.Audience == "" {
		return errors.New("gcp_token: audience is required")
	}

	if g.TokenURL == "" {
		g.TokenURL = defaultTokenURL
	}

	if g.Cert == "" || g.Key == "" {
		return errors.New("gcp_token: cert and key are required")
	}

	if len(g.Scopes) == 0 {
		g.Scopes = []string{scopeCloudPlatform}
	}

	if g.Lifetime <= 0 {
		g.Lifetime = defaultLifetime
	}

	if g.RefreshBefore <= 0 {
		g.RefreshBefore = defaultRefreshBefore
	}

	if g.Timeout <= 0 {
		g.Timeout = defaultTimeout
	}

	switch g.Format {
	case "":
		g.Format = FormatRaw
	case FormatRaw:
	case FormatManagedKafka:
		if g.serviceAccountEmail() == "" {
			return errors.New("gcp_token: managed_kafka format requires service_account_impersonation_url")
		}
	default:
		return fmt.Errorf("gcp_token: unknown format %q", g.Format)
	}

	switch g.Mode {
	case "":
		g.Mode = ModeEndpoint
	case ModeEndpoint, ModeInject:
	default:
		return fmt.Errorf("gcp_token: unknown mode %q", g.Mode)
	}

	if g.Header == "" {
		g.Header = "Authorization"
	}

	if (g.ClientID == "") != (g.ClientSecret == "") {
		return errors.New("gcp_token: client_id and client_secret must be set together")
	}

	if g.now == nil {
		g.now = time.Now
	}

	if g.client == nil {
		c, err := g.newHTTPClient()
		if err != nil {
			return err
		}

		g.client = c
	}

	return nil
}

func (g *GCPToken) newHTTPClient() (*http.Client, error) {
	cert, err := tls.X509KeyPair([]byte(g.Cert), []byte(g.Key))
	if err != nil {
		return nil, fmt.Errorf("gcp_token: load cert/key: %w", err)
	}

	tlsCfg := &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
	}

	if g.CA != "" {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM([]byte(g.CA)) {
			return nil, errors.New("gcp_token: no valid certificate in ca")
		}

		tlsCfg.RootCAs = pool
	}

	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = tlsCfg

	return &http.Client{Transport: tr, Timeout: g.Timeout}, nil
}

// subjectToken builds the X.509 subject token: JSON array of base64 DER certs, leaf first.
func (g *GCPToken) subjectToken() (string, error) {
	var certs []string

	rest := []byte(g.Cert)
	for {
		var block *pem.Block

		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}

		if block.Type == "CERTIFICATE" {
			certs = append(certs, base64.StdEncoding.EncodeToString(block.Bytes))
		}
	}

	if len(certs) == 0 {
		return "", errors.New("gcp_token: no certificate found in cert")
	}

	b, err := json.Marshal(certs)

	return string(b), err
}

func (g *GCPToken) serviceAccountEmail() string {
	u := g.ServiceAccountImpersonationURL

	i := strings.LastIndex(u, impersonationURLPattern)
	if i < 0 {
		return ""
	}

	s := u[i+len(impersonationURLPattern):]
	if j := strings.Index(s, ":"); j >= 0 {
		s = s[:j]
	}

	return s
}

// Token returns a cached token, refreshing when needed.
func (g *GCPToken) Token(ctx context.Context) (*Token, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.cached != nil && g.now().Add(g.RefreshBefore).Before(g.cached.Expiry) {
		return g.cached, nil
	}

	t, err := g.fetch(ctx)
	if err != nil {
		// serve still-valid stale token if refresh fails
		if g.cached != nil && g.now().Before(g.cached.Expiry) {
			slog.Warn("gcp_token: refresh failed, using cached token", "err", err.Error())

			return g.cached, nil
		}

		return nil, err
	}

	g.cached = t

	return t, nil
}

func (g *GCPToken) fetch(ctx context.Context) (*Token, error) {
	if g.hasCertFiles() {
		changed, err := g.loadCertFiles()
		if err != nil {
			return nil, err
		}

		if changed {
			c, err := g.newHTTPClient()
			if err != nil {
				return nil, err
			}

			g.client = c

			slog.Info("gcp_token: certificate files reloaded")
		}
	}

	subject, err := g.subjectToken()
	if err != nil {
		return nil, err
	}

	form := url.Values{
		"grant_type":           {grantTypeTokenExchange},
		"audience":             {g.Audience},
		"scope":                {strings.Join(g.Scopes, " ")},
		"requested_token_type": {tokenTypeAccessToken},
		"subject_token":        {subject},
		"subject_token_type":   {tokenTypeMTLS},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	var sts struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}

	if err := g.do(req, &sts); err != nil {
		return nil, fmt.Errorf("gcp_token: sts exchange: %w", err)
	}

	if sts.AccessToken == "" {
		return nil, errors.New("gcp_token: sts exchange: empty access_token")
	}

	if g.ServiceAccountImpersonationURL == "" {
		return &Token{
			AccessToken: sts.AccessToken,
			Expiry:      g.now().Add(time.Duration(sts.ExpiresIn) * time.Second),
		}, nil
	}

	body, err := json.Marshal(map[string]any{
		"scope":    g.Scopes,
		"lifetime": strconv.FormatInt(int64(g.Lifetime.Seconds()), 10) + "s",
	})
	if err != nil {
		return nil, err
	}

	req, err = http.NewRequestWithContext(ctx, http.MethodPost, g.ServiceAccountImpersonationURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+sts.AccessToken)

	var imp struct {
		AccessToken string    `json:"accessToken"`
		ExpireTime  time.Time `json:"expireTime"`
	}

	if err := g.do(req, &imp); err != nil {
		return nil, fmt.Errorf("gcp_token: impersonation: %w", err)
	}

	if imp.AccessToken == "" {
		return nil, errors.New("gcp_token: impersonation: empty accessToken")
	}

	return &Token{AccessToken: imp.AccessToken, Expiry: imp.ExpireTime}, nil
}

func (g *GCPToken) do(req *http.Request, v any) error {
	resp, err := g.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return err
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	return json.Unmarshal(b, v)
}

// format converts the Google access token to the configured output format.
func (g *GCPToken) format(t *Token) (string, error) {
	if g.Format != FormatManagedKafka {
		return t.AccessToken, nil
	}

	enc := base64.RawURLEncoding

	header, err := json.Marshal(map[string]string{"typ": "JWT", "alg": "GOOG_OAUTH2_TOKEN"})
	if err != nil {
		return "", err
	}

	payload, err := json.Marshal(map[string]any{
		"exp":   t.Expiry.Unix(),
		"iat":   g.now().Unix(),
		"iss":   "Google",
		"scope": "kafka",
		"sub":   g.serviceAccountEmail(),
	})
	if err != nil {
		return "", err
	}

	return enc.EncodeToString(header) + "." + enc.EncodeToString(payload) + "." +
		enc.EncodeToString([]byte(t.AccessToken)), nil
}

func (g *GCPToken) authorized(r *http.Request) bool {
	if g.ClientID == "" {
		return true
	}

	id, secret, ok := r.BasicAuth()
	if !ok {
		if err := r.ParseForm(); err == nil {
			id, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
		}
	}

	idOK := subtle.ConstantTimeCompare([]byte(id), []byte(g.ClientID)) == 1
	secretOK := subtle.ConstantTimeCompare([]byte(secret), []byte(g.ClientSecret)) == 1

	return idOK && secretOK
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (g *GCPToken) Middleware() (func(http.Handler) http.Handler, error) {
	if err := g.init(); err != nil {
		return nil, err
	}

	if _, err := g.subjectToken(); err != nil {
		return nil, err
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t, err := g.Token(r.Context())
			if err != nil {
				slog.Error("gcp_token: cannot get token", "err", err.Error())

				if g.Mode == ModeInject {
					http.Error(w, "cannot get upstream token", http.StatusBadGateway)
					return
				}

				writeJSON(w, http.StatusBadGateway, map[string]string{
					"error":             "temporarily_unavailable",
					"error_description": "cannot get token from google",
				})

				return
			}

			token, err := g.format(t)
			if err != nil {
				http.Error(w, "cannot format token", http.StatusInternalServerError)
				return
			}

			if g.Mode == ModeInject {
				value := token
				if strings.EqualFold(g.Header, "Authorization") {
					value = "Bearer " + token
				}

				r.Header.Set(g.Header, value)
				next.ServeHTTP(w, r)

				return
			}

			if !g.authorized(r) {
				w.Header().Set("WWW-Authenticate", `Basic realm="gcp_token"`)
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_client"})

				return
			}

			expiresIn := int64(t.Expiry.Sub(g.now()).Seconds())
			if expiresIn < 0 {
				expiresIn = 0
			}

			writeJSON(w, http.StatusOK, map[string]any{
				"access_token": token,
				"token_type":   "Bearer",
				"expires_in":   expiresIn,
			})
		})
	}, nil
}
