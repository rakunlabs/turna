package ldapview

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/go-ldap/ldap/v3"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/folder"
)

//go:embed _ui/dist/*
var uiFS embed.FS

const (
	apiPath = "/api"

	defaultSizeLimit    = 500
	defaultResponseSize = 16 << 20
	defaultMaxSessions  = 100
	defaultTimeLimit    = 30 * time.Second
	defaultIdleTimeout  = 30 * time.Minute
	defaultDialTimeout  = 10 * time.Second
)

// Bind modes a user can pick on the login screen.
const (
	BindSimple          = "simple"
	BindAnonymous       = "anonymous"
	BindUnauthenticated = "unauthenticated"
)

// LdapView ships a UI to log in to an LDAP server with the user's own
// credentials, anonymously or with an unauthenticated bind, and run searches.
type LdapView struct {
	// Servers are presets offered on the login screen; users can always type
	// any other LDAP URL.
	Servers []Server `cfg:"servers"`
	// DisableBindModes hides bind modes, any of simple, anonymous, unauthenticated.
	DisableBindModes []string `cfg:"disable_bind_modes"`
	// PrefixPath is stripped from the request path.
	PrefixPath string `cfg:"prefix_path"`
	// Title is shown in the UI header.
	Title string `cfg:"title"`
	// MaxSizeLimit caps the entries a search returns. Default is 500.
	MaxSizeLimit int `cfg:"max_size_limit"`
	// MaxResponseSize caps the size of one search response in bytes; entries
	// past it are dropped and the result is marked truncated. Default is 16MB.
	MaxResponseSize int `cfg:"max_response_size"`
	// MaxTimeLimit caps a search duration. Default is 30s.
	MaxTimeLimit time.Duration `cfg:"max_time_limit"`
	// IdleTimeout closes a session's LDAP connection after inactivity. Default is 30m.
	IdleTimeout time.Duration `cfg:"idle_timeout"`
	// MaxSessions caps open sessions, each holding an LDAP connection; the
	// least recently used one is closed when a new login needs room. Default is 100.
	MaxSessions int `cfg:"max_sessions"`
	// DialTimeout limits connecting to the server. Default is 10s.
	DialTimeout time.Duration `cfg:"dial_timeout"`
	// CookieName holds the session id. Default is turna_ldap_view.
	CookieName string `cfg:"cookie_name"`
	// SecureCookie sets the Secure flag on the session cookie.
	SecureCookie bool `cfg:"secure_cookie"`

	ui       http.Handler
	sessions *sessionStore
}

type Server struct {
	// Name is shown on the login screen; defaults to Addr.
	Name string `cfg:"name"`
	// Addr is an LDAP URL, ldap://host:389 or ldaps://host:636.
	Addr string `cfg:"addr"`
	// BaseDN prefills the search base.
	BaseDN string `cfg:"base_dn"`
	// StartTLS upgrades an ldap:// connection.
	StartTLS bool `cfg:"start_tls"`
	// InsecureSkipVerify skips TLS certificate checks.
	InsecureSkipVerify bool `cfg:"insecure_skip_verify"`
	// CAFile is a PEM file with CA certificates trusted in addition to the
	// system ones, for servers with an internal CA.
	CAFile string `cfg:"ca_file"`
	// CA is the same as CAFile, given inline as PEM.
	CA string `cfg:"ca"`
	// UserDNTemplate turns a plain username into a bind DN, e.g.
	// "uid=%s,ou=people,dc=example,dc=com". Usernames with "=" or "@" are sent as is.
	UserDNTemplate string `cfg:"user_dn_template"`

	rootCAs *x509.CertPool
}

func (m *LdapView) Middleware() (func(http.Handler) http.Handler, error) {
	m.PrefixPath = "/" + strings.Trim(m.PrefixPath, "/")
	if m.PrefixPath == "/" {
		m.PrefixPath = ""
	}

	if m.MaxSizeLimit <= 0 {
		m.MaxSizeLimit = defaultSizeLimit
	}

	if m.MaxResponseSize <= 0 {
		m.MaxResponseSize = defaultResponseSize
	}

	if m.MaxTimeLimit <= 0 {
		m.MaxTimeLimit = defaultTimeLimit
	}

	if m.IdleTimeout <= 0 {
		m.IdleTimeout = defaultIdleTimeout
	}

	if m.MaxSessions <= 0 {
		m.MaxSessions = defaultMaxSessions
	}

	if m.DialTimeout <= 0 {
		m.DialTimeout = defaultDialTimeout
	}

	if m.CookieName == "" {
		m.CookieName = "turna_ldap_view"
	}

	for _, mode := range m.DisableBindModes {
		switch mode {
		case BindSimple, BindAnonymous, BindUnauthenticated:
		default:
			return nil, fmt.Errorf("ldap_view: unknown bind mode %q in disable_bind_modes", mode)
		}
	}

	if len(m.bindModes()) == 0 {
		return nil, errors.New("ldap_view: every bind mode is disabled")
	}

	for i, s := range m.Servers {
		if s.Addr == "" {
			return nil, fmt.Errorf("ldap_view: servers[%d].addr is required", i)
		}

		if err := validAddr(s.Addr); err != nil {
			return nil, fmt.Errorf("ldap_view: servers[%d]: %w", i, err)
		}

		if t := s.UserDNTemplate; t != "" && (strings.Count(t, "%") != 1 || !strings.Contains(t, "%s")) {
			return nil, fmt.Errorf("ldap_view: servers[%d].user_dn_template needs exactly one %%s", i)
		}

		if s.Name == "" {
			m.Servers[i].Name = s.Addr
		}

		pem := []byte(s.CA)
		if s.CAFile != "" {
			b, err := os.ReadFile(s.CAFile)
			if err != nil {
				return nil, fmt.Errorf("ldap_view: servers[%d].ca_file: %w", i, err)
			}

			pem = append(append(pem, '\n'), b...)
		}

		pool, err := certPool(pem)
		if err != nil {
			return nil, fmt.Errorf("ldap_view: servers[%d]: %w", i, err)
		}

		m.Servers[i].rootCAs = pool
	}

	sub, err := fs.Sub(uiFS, "_ui/dist")
	if err != nil {
		return nil, err
	}

	f := folder.Folder{
		Index: true,
		CacheRegex: []*folder.RegexCacheStore{
			{Regex: `index\.html$`, CacheControl: "no-store"},
			{Regex: `.*`, CacheControl: "public, max-age=259200"},
		},
	}
	f.SetFs(http.FS(sub))

	uiM, err := f.Middleware()
	if err != nil {
		return nil, err
	}

	m.ui = uiM(nil)
	m.sessions = newSessionStore(m.IdleTimeout, m.MaxSessions)

	return func(_ http.Handler) http.Handler {
		return http.HandlerFunc(m.serveHTTP)
	}, nil
}

func (m *LdapView) bindModes() []string {
	modes := make([]string, 0, 3)
	for _, mode := range []string{BindSimple, BindAnonymous, BindUnauthenticated} {
		disabled := false
		for _, d := range m.DisableBindModes {
			if d == mode {
				disabled = true

				break
			}
		}

		if !disabled {
			modes = append(modes, mode)
		}
	}

	return modes
}

func (m *LdapView) serveHTTP(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	if m.PrefixPath != "" {
		if p != m.PrefixPath && !strings.HasPrefix(p, m.PrefixPath+"/") {
			http.NotFound(w, r)

			return
		}

		p = strings.TrimPrefix(p, m.PrefixPath)
	}

	if p == "" {
		http.Redirect(w, r, m.PrefixPath+"/", http.StatusTemporaryRedirect)

		return
	}

	if rest, ok := strings.CutPrefix(p, apiPath+"/"); ok {
		m.serveAPI(w, r, rest)

		return
	}

	r2 := r.Clone(r.Context())
	r2.URL.Path = p
	m.ui.ServeHTTP(w, r2)
}

func validAddr(addr string) error {
	u, err := url.Parse(addr)
	if err != nil {
		return fmt.Errorf("invalid address %q: %w", addr, err)
	}

	switch u.Scheme {
	case "ldap", "ldaps", "ldapi":
	default:
		return fmt.Errorf("address %q must start with ldap://, ldaps:// or ldapi://", addr)
	}

	if u.Host == "" && u.Scheme != "ldapi" {
		return fmt.Errorf("address %q has no host", addr)
	}

	return nil
}

type connectOptions struct {
	Addr               string
	StartTLS           bool
	InsecureSkipVerify bool
	// RootCAs is nil for the system pool.
	RootCAs  *x509.CertPool
	Mode     string
	Username string
	Password string
}

// certPool returns the system pool with the PEM certificates added, or nil
// for the plain system pool when pem is empty.
func certPool(pem []byte) (*x509.CertPool, error) {
	if len(bytes.TrimSpace(pem)) == 0 {
		return nil, nil //nolint:nilnil // nil selects the system pool
	}

	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}

	if !pool.AppendCertsFromPEM(pem) {
		return nil, errors.New("CA has no PEM certificate")
	}

	return pool, nil
}

func (m *LdapView) connect(o connectOptions) (*ldap.Conn, error) {
	if err := validAddr(o.Addr); err != nil {
		return nil, err
	}

	u, _ := url.Parse(o.Addr)
	tlsConfig := &tls.Config{
		ServerName:         u.Hostname(),
		InsecureSkipVerify: o.InsecureSkipVerify, //nolint:gosec // opt-in for test servers
		RootCAs:            o.RootCAs,
		MinVersion:         tls.VersionTLS12,
	}

	conn, err := ldap.DialURL(o.Addr,
		ldap.DialWithTLSConfig(tlsConfig),
		ldap.DialWithDialer(&net.Dialer{Timeout: m.DialTimeout}),
	)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}

	conn.SetTimeout(m.MaxTimeLimit)

	if o.StartTLS && u.Scheme == "ldap" {
		if err := conn.StartTLS(tlsConfig); err != nil {
			_ = conn.Close()

			return nil, fmt.Errorf("starttls: %w", err)
		}
	}

	switch o.Mode {
	case BindSimple:
		if o.Username == "" || o.Password == "" {
			_ = conn.Close()

			return nil, errors.New("simple bind needs a username and a password")
		}

		_, err = conn.SimpleBind(ldap.NewSimpleBindRequest(o.Username, o.Password, nil))
	case BindAnonymous:
		err = conn.UnauthenticatedBind("")
	case BindUnauthenticated:
		if o.Username == "" {
			_ = conn.Close()

			return nil, errors.New("unauthenticated bind needs a username")
		}

		err = conn.UnauthenticatedBind(o.Username)
	default:
		err = fmt.Errorf("unknown bind mode %q", o.Mode)
	}

	if err != nil {
		_ = conn.Close()

		return nil, fmt.Errorf("bind: %w", err)
	}

	return conn, nil
}

func userDN(template, username string) string {
	if template == "" || username == "" || strings.ContainsAny(username, "=@\\") {
		return username
	}

	return fmt.Sprintf(template, ldap.EscapeDN(username))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

type apiError struct {
	Error string `json:"error"`
	Code  int    `json:"code,omitempty"`
}

// errText drops the trailing ": " go-ldap leaves when the server sends no
// diagnostic message.
func errText(err error) string {
	return strings.TrimSuffix(strings.TrimSpace(err.Error()), ":")
}

func writeError(w http.ResponseWriter, status int, err error) {
	res := apiError{Error: errText(err)}

	var lerr *ldap.Error
	if errors.As(err, &lerr) {
		res.Code = int(lerr.ResultCode)
	}

	writeJSON(w, status, res)
}
