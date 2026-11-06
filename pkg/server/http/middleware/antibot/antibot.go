// Package antibot challenges clients before they reach the application, like
// the BunkerWeb antibot. Clients solve a JavaScript proof of work or a captcha
// and receive a signed cookie that lets them through until it expires.
package antibot

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"math/bits"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	adaproxy "github.com/rakunlabs/ada/middleware/auth/proxy"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/ipcheck"
)

type Antibot struct {
	// Type is javascript, turnstile, hcaptcha or recaptcha; default
	// javascript.
	Type string `cfg:"type"`
	// Secret signs the pass cookie and challenges. Without it a random one is
	// generated at start, which is not shared between instances.
	Secret string `cfg:"secret" log:"-"`

	Cookie Cookie `cfg:"cookie"`
	// BindIP binds the cookie to the client IP, default true.
	BindIP *bool `cfg:"bind_ip"`
	// TrustedProxies are CIDRs whose forwarding headers are used to find the
	// client IP.
	TrustedProxies []string `cfg:"trusted_proxies"`

	// Path of the challenge endpoint, default /.well-known/turna/antibot.
	Path string `cfg:"path"`
	// SkipPaths are path prefixes that are not challenged.
	SkipPaths []string `cfg:"skip_paths"`
	// Allow lists IPs or CIDRs that are not challenged.
	Allow []string `cfg:"allow"`

	// SiteKey and SecretKey of the captcha provider.
	SiteKey   string `cfg:"site_key"`
	SecretKey string `cfg:"secret_key" log:"-"`
	// VerifyURL overrides the siteverify URL of the captcha provider.
	VerifyURL string `cfg:"verify_url"`

	// Difficulty of the javascript proof of work in leading zero bits,
	// default 16.
	Difficulty int `cfg:"difficulty"`
	// ChallengeTTL is how long a javascript challenge can be solved, default
	// 5m.
	ChallengeTTL time.Duration `cfg:"challenge_ttl"`

	// ChallengeStatus of the challenge page, default 403.
	ChallengeStatus int `cfg:"challenge_status"`
	// StatusCode of rejected non HTML requests, default 403.
	StatusCode int `cfg:"status_code"`

	// Title and Message of the challenge page.
	Title   string `cfg:"title"`
	Message string `cfg:"message"`
}

type Cookie struct {
	// Name default is turna_antibot.
	Name string `cfg:"name"`
	// MaxAge is how long a solved challenge is valid, default 24h.
	MaxAge time.Duration `cfg:"max_age"`
	// Secure default is true.
	Secure *bool `cfg:"secure"`
	// SameSite is lax, strict or none; default is lax.
	SameSite string `cfg:"same_site"`
	Domain   string `cfg:"domain"`
	// Path default is /.
	Path string `cfg:"path"`
}

type provider struct {
	script    string
	widget    string
	field     string
	verifyURL string
}

var providers = map[string]provider{
	"turnstile": {
		script:    "https://challenges.cloudflare.com/turnstile/v0/api.js",
		widget:    "cf-turnstile",
		field:     "cf-turnstile-response",
		verifyURL: "https://challenges.cloudflare.com/turnstile/v0/siteverify",
	},
	"hcaptcha": {
		script:    "https://js.hcaptcha.com/1/api.js",
		widget:    "h-captcha",
		field:     "h-captcha-response",
		verifyURL: "https://api.hcaptcha.com/siteverify",
	},
	"recaptcha": {
		script:    "https://www.google.com/recaptcha/api.js",
		widget:    "g-recaptcha",
		field:     "g-recaptcha-response",
		verifyURL: "https://www.google.com/recaptcha/api/siteverify",
	},
}

const (
	typeJavascript = "javascript"
	defaultPath    = "/.well-known/turna/antibot"
	maxDifficulty  = 32
)

type antibot struct {
	typ      string
	provider provider
	secret   []byte

	cookie     http.Cookie
	cookieTTL  time.Duration
	bindIP     bool
	policy     adaproxy.Policy
	path       string
	skipPaths  []string
	allow      *ipcheck.Checker
	siteKey    string
	secretKey  string
	difficulty int
	powTTL     time.Duration

	challengeStatus int
	status          int
	title, message  string

	client *http.Client
	now    func() time.Time
}

func (m *Antibot) build() (*antibot, error) {
	a := &antibot{
		typ:             strings.ToLower(strings.TrimSpace(m.Type)),
		bindIP:          m.BindIP == nil || *m.BindIP,
		path:            m.Path,
		skipPaths:       m.SkipPaths,
		siteKey:         m.SiteKey,
		secretKey:       m.SecretKey,
		difficulty:      m.Difficulty,
		powTTL:          m.ChallengeTTL,
		cookieTTL:       m.Cookie.MaxAge,
		challengeStatus: m.ChallengeStatus,
		status:          m.StatusCode,
		title:           m.Title,
		message:         m.Message,
		client:          &http.Client{Timeout: 10 * time.Second},
		now:             time.Now,
	}

	if a.typ == "" {
		a.typ = typeJavascript
	}

	if a.typ != typeJavascript {
		p, ok := providers[a.typ]
		if !ok {
			return nil, fmt.Errorf("antibot unsupported type %q", m.Type)
		}

		if a.siteKey == "" || a.secretKey == "" {
			return nil, fmt.Errorf("antibot type %s requires site_key and secret_key", a.typ)
		}

		if m.VerifyURL != "" {
			u, err := url.Parse(m.VerifyURL)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				return nil, fmt.Errorf("antibot verify_url %q is invalid", m.VerifyURL)
			}

			p.verifyURL = m.VerifyURL
		}

		a.provider = p
	}

	if m.Secret != "" {
		if len(m.Secret) < 16 {
			return nil, errors.New("antibot secret must be at least 16 bytes")
		}

		a.secret = []byte(m.Secret)
	} else {
		a.secret = make([]byte, 32)
		_, _ = rand.Read(a.secret)

		slog.Warn("antibot secret is not set, using a random one; pass cookies are not shared between instances or restarts")
	}

	if a.path == "" {
		a.path = defaultPath
	}
	if !strings.HasPrefix(a.path, "/") {
		return nil, errors.New("antibot path must start with /")
	}

	if a.difficulty == 0 {
		a.difficulty = 16
	}
	if a.difficulty < 1 || a.difficulty > maxDifficulty {
		return nil, fmt.Errorf("antibot difficulty must be between 1 and %d", maxDifficulty)
	}

	if a.powTTL == 0 {
		a.powTTL = 5 * time.Minute
	}
	if a.cookieTTL == 0 {
		a.cookieTTL = 24 * time.Hour
	}
	if a.powTTL < 0 || a.cookieTTL < 0 {
		return nil, errors.New("antibot durations must not be negative")
	}

	if a.challengeStatus == 0 {
		a.challengeStatus = http.StatusForbidden
	}
	if a.status == 0 {
		a.status = http.StatusForbidden
	}
	if a.challengeStatus < 200 || a.challengeStatus > 599 || a.status < 200 || a.status > 599 {
		return nil, errors.New("antibot challenge_status and status_code must be between 200 and 599")
	}
	if a.title == "" {
		a.title = "Checking your browser"
	}
	if a.message == "" {
		a.message = "Please wait while we verify that you are not a bot."
		if a.typ != typeJavascript {
			a.message = "Please complete the challenge to continue."
		}
	}

	var err error
	if a.policy, err = adaproxy.New(m.TrustedProxies...); err != nil {
		return nil, fmt.Errorf("antibot trusted_proxies: %w", err)
	}

	if len(m.Allow) > 0 {
		if a.allow, err = ipcheck.NewChecker(m.Allow); err != nil {
			return nil, fmt.Errorf("antibot allow: %w", err)
		}
	}

	secure := m.Cookie.Secure == nil || *m.Cookie.Secure

	var sameSite http.SameSite
	switch strings.ToLower(m.Cookie.SameSite) {
	case "", "lax":
		sameSite = http.SameSiteLaxMode
	case "strict":
		sameSite = http.SameSiteStrictMode
	case "none":
		sameSite = http.SameSiteNoneMode
		if !secure {
			return nil, errors.New("antibot cookie same_site none requires secure")
		}
	default:
		return nil, fmt.Errorf("antibot unsupported cookie same_site %q", m.Cookie.SameSite)
	}

	a.cookie = http.Cookie{
		Name:     m.Cookie.Name,
		Path:     m.Cookie.Path,
		Domain:   m.Cookie.Domain,
		Secure:   secure,
		HttpOnly: true,
		SameSite: sameSite,
	}
	if a.cookie.Name == "" {
		a.cookie.Name = "turna_antibot"
	}
	if a.cookie.Path == "" {
		a.cookie.Path = "/"
	}

	return a, nil
}

func (m *Antibot) Middleware(_ context.Context) (func(http.Handler) http.Handler, error) {
	a, err := m.build()
	if err != nil {
		return nil, err
	}

	return a.middleware, nil
}

func (a *antibot) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == a.path {
			a.serveChallenge(w, r)

			return
		}

		if a.skip(r.URL.Path) {
			next.ServeHTTP(w, r)

			return
		}

		ip, err := a.policy.ClientIP(r)
		if err != nil {
			// malformed forwarding headers from a trusted proxy
			httputil.HandleError(w, httputil.NewError("client ip is invalid", nil, http.StatusBadRequest))

			return
		}

		if a.allow != nil {
			if ok, _ := a.allow.Contains(ip); ok {
				next.ServeHTTP(w, r)

				return
			}
		}

		if c, err := r.Cookie(a.cookie.Name); err == nil && a.validCookie(c.Value, ip) {
			next.ServeHTTP(w, r)

			return
		}

		if (r.Method == http.MethodGet || r.Method == http.MethodHead) &&
			strings.Contains(r.Header.Get("Accept"), "text/html") {
			a.renderPage(w, r, ip, r.URL.RequestURI(), "")

			return
		}

		httputil.HandleError(w, httputil.NewError("antibot challenge required", nil, a.status))
	})
}

// skip reports whether p is under a skip path, also after cleaning, so
// "/health/../admin" is not skipped for an upstream that normalizes it.
func (a *antibot) skip(p string) bool {
	if len(a.skipPaths) == 0 {
		return false
	}

	clean := path.Clean("/" + p)
	if clean != "/" && strings.HasSuffix(p, "/") {
		clean += "/"
	}

	for _, prefix := range a.skipPaths {
		if strings.HasPrefix(p, prefix) && strings.HasPrefix(clean, prefix) {
			return true
		}
	}

	return false
}

func (a *antibot) mac(parts ...string) []byte {
	h := hmac.New(sha256.New, a.secret)
	h.Write([]byte(strings.Join(parts, "\n")))

	return h.Sum(nil)
}

func (a *antibot) boundIP(ip string) string {
	if a.bindIP {
		return ip
	}

	return ""
}

// cookieValue is "<unix expiry>.<signature>"; the IP is only part of the
// signature, so it is not exposed in the cookie.
func (a *antibot) cookieValue(ip string, expires time.Time) string {
	exp := strconv.FormatInt(expires.Unix(), 10)

	return exp + "." + base64.RawURLEncoding.EncodeToString(a.mac("pass", exp, a.boundIP(ip)))
}

func (a *antibot) validCookie(value, ip string) bool {
	exp, sig, ok := strings.Cut(value, ".")
	if !ok {
		return false
	}

	until, err := strconv.ParseInt(exp, 10, 64)
	if err != nil || !a.now().Before(time.Unix(until, 0)) {
		return false
	}

	// a cookie claiming a longer life than configured was not issued here
	if time.Unix(until, 0).Sub(a.now()) > a.cookieTTL+time.Minute {
		return false
	}

	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil {
		return false
	}

	return hmac.Equal(got, a.mac("pass", exp, a.boundIP(ip)))
}

// newChallenge returns "<unix expiry>.<random>.<signature>" for the
// javascript proof of work, so the server keeps no state.
func (a *antibot) newChallenge(ip string) string {
	nonce := make([]byte, 16)
	_, _ = rand.Read(nonce)

	exp := strconv.FormatInt(a.now().Add(a.powTTL).Unix(), 10)
	random := base64.RawURLEncoding.EncodeToString(nonce)
	sig := a.mac("pow", exp, random, strconv.Itoa(a.difficulty), a.boundIP(ip))

	return exp + "." + random + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func (a *antibot) verifyPoW(challenge, nonce, ip string) error {
	parts := strings.Split(challenge, ".")
	if len(parts) != 3 {
		return errors.New("challenge is invalid")
	}

	until, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return errors.New("challenge is invalid")
	}

	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(sig, a.mac("pow", parts[0], parts[1], strconv.Itoa(a.difficulty), a.boundIP(ip))) {
		return errors.New("challenge is invalid")
	}

	if !a.now().Before(time.Unix(until, 0)) {
		return errors.New("challenge expired")
	}

	if nonce == "" || len(nonce) > 20 {
		return errors.New("solution is invalid")
	}
	if _, err := strconv.ParseUint(nonce, 10, 64); err != nil {
		return errors.New("solution is invalid")
	}

	if leadingZeroBits(sha256.Sum256([]byte(challenge+nonce))) < a.difficulty {
		return errors.New("solution is invalid")
	}

	return nil
}

func leadingZeroBits(sum [sha256.Size]byte) int {
	n := 0
	for _, b := range sum {
		if b != 0 {
			return n + bits.LeadingZeros8(b)
		}

		n += 8
	}

	return n
}

func (a *antibot) verifyCaptcha(ctx context.Context, token, ip string) error {
	if token == "" {
		return errors.New("captcha response missing")
	}

	form := url.Values{
		"secret":   {a.secretKey},
		"response": {token},
	}
	if ip != "" {
		form.Set("remoteip", ip)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.provider.verifyURL, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("captcha verify: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("captcha verify status %d", resp.StatusCode)
	}

	var result struct {
		Success bool `json:"success"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&result); err != nil {
		return fmt.Errorf("captcha verify: %w", err)
	}

	if !result.Success {
		return errors.New("captcha failed")
	}

	return nil
}

// safeRedirect returns target when it is a path on this site, else "/".
func safeRedirect(target string) string {
	if target == "" || target[0] != '/' || strings.HasPrefix(target, "//") ||
		strings.ContainsAny(target, "\\\r\n\t") {
		return "/"
	}

	u, err := url.Parse(target)
	if err != nil || u.Scheme != "" || u.Host != "" || u.User != nil {
		return "/"
	}

	return target
}

func (a *antibot) serveChallenge(w http.ResponseWriter, r *http.Request) {
	ip, err := a.policy.ClientIP(r)
	if err != nil {
		httputil.HandleError(w, httputil.NewError("client ip is invalid", nil, http.StatusBadRequest))

		return
	}

	switch r.Method {
	case http.MethodGet, http.MethodHead:
		a.renderPage(w, r, ip, r.URL.Query().Get("redirect"), "")

		return
	case http.MethodPost:
	default:
		w.Header().Set("Allow", "GET, HEAD, POST")
		httputil.HandleError(w, httputil.NewError("method not allowed", nil, http.StatusMethodNotAllowed))

		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		httputil.HandleError(w, httputil.NewError("form is invalid", nil, http.StatusBadRequest))

		return
	}

	redirect := safeRedirect(r.PostForm.Get("redirect"))

	if a.typ == typeJavascript {
		err = a.verifyPoW(r.PostForm.Get("challenge"), r.PostForm.Get("nonce"), ip)
	} else {
		err = a.verifyCaptcha(r.Context(), r.PostForm.Get(a.provider.field), ip)
	}

	if err != nil {
		slog.Debug("antibot verification failed", "ip", ip, "error", err)
		a.renderPage(w, r, ip, redirect, "Verification failed, please try again.")

		return
	}

	c := a.cookie
	c.Value = a.cookieValue(ip, a.now().Add(a.cookieTTL))
	c.MaxAge = int(a.cookieTTL / time.Second)
	http.SetCookie(w, &c)

	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, redirect, http.StatusSeeOther)
}

type pageData struct {
	Title      string
	Message    string
	Error      string
	Action     string
	Redirect   string
	Javascript bool
	Challenge  string
	Difficulty int
	Script     string
	Widget     string
	SiteKey    string
}

func (a *antibot) renderPage(w http.ResponseWriter, r *http.Request, ip, redirect, errMsg string) {
	data := pageData{
		Title:      a.title,
		Message:    a.message,
		Error:      errMsg,
		Action:     a.path,
		Redirect:   safeRedirect(redirect),
		Javascript: a.typ == typeJavascript,
		Difficulty: a.difficulty,
		Script:     a.provider.script,
		Widget:     a.provider.widget,
		SiteKey:    a.siteKey,
	}
	if data.Javascript {
		data.Challenge = a.newChallenge(ip)
	}

	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Robots-Tag", "noindex")

	w.WriteHeader(a.challengeStatus)

	if r.Method == http.MethodHead {
		return
	}

	if err := pageTemplate.Execute(w, data); err != nil {
		slog.Warn("antibot render page", "error", err)
	}
}

var pageTemplate = template.Must(template.New("antibot").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex">
<title>{{.Title}}</title>
<style>
body{font-family:system-ui,sans-serif;display:flex;align-items:center;justify-content:center;min-height:100vh;margin:0;background:#f6f7f9;color:#1f2328}
main{max-width:28rem;padding:2rem;text-align:center}
.error{color:#b42318}
form>div{display:inline-block;margin-top:1rem}
</style>
{{- if not .Javascript}}
<script src="{{.Script}}" async defer></script>
{{- end}}
</head>
<body>
<main>
<h1>{{.Title}}</h1>
<p>{{.Message}}</p>
{{- if .Error}}
<p class="error">{{.Error}} <a href="{{.Redirect}}">Retry</a></p>
{{- end}}
<form id="antibot" method="post" action="{{.Action}}">
<input type="hidden" name="redirect" value="{{.Redirect}}">
{{- if .Javascript}}
<input type="hidden" name="challenge" value="{{.Challenge}}">
<input type="hidden" name="nonce" value="">
<p id="status"></p>
<noscript><p class="error">JavaScript is required to continue.</p></noscript>
{{- else}}
<div class="{{.Widget}}" data-sitekey="{{.SiteKey}}" data-callback="antibotPass"></div>
{{- end}}
</form>
</main>
{{- if .Javascript}}
{{- if not .Error}}
<script>
(async function () {
  var form = document.getElementById("antibot");
  var status = document.getElementById("status");
  var difficulty = {{.Difficulty}};
  if (!window.crypto || !window.crypto.subtle) {
    status.textContent = "Your browser cannot complete the check, a secure (https) connection is required.";
    return;
  }
  var challenge = form.elements.challenge.value;
  var encoder = new TextEncoder();
  function zeros(hash) {
    var n = 0;
    for (var i = 0; i < hash.length; i++) {
      if (hash[i] === 0) { n += 8; continue; }
      return n + Math.clz32(hash[i]) - 24;
    }
    return n;
  }
  for (var nonce = 0; ; nonce++) {
    var hash = new Uint8Array(await crypto.subtle.digest("SHA-256", encoder.encode(challenge + nonce)));
    if (zeros(hash) >= difficulty) {
      form.elements.nonce.value = String(nonce);
      form.submit();
      return;
    }
  }
})();
</script>
{{- end}}
{{- else}}
<script>
function antibotPass() { document.getElementById("antibot").submit(); }
</script>
{{- end}}
</body>
</html>
`))
