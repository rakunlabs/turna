package login

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/rakunlabs/into"
	"github.com/rakunlabs/ok"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/oauth2/auth"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/oauth2/store"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/session"
	"github.com/rakunlabs/turna/pkg/server/http/tcontext"
)

// Login middleware gives a login page.
type Login struct {
	Path     Path     `cfg:"path"`
	Redirect Redirect `cfg:"redirect"`
	UI       UI       `cfg:"ui"`
	Info     Info     `cfg:"info"`
	Request  Request  `cfg:"request"`

	SessionMiddleware string `cfg:"session_middleware"`

	StateCookie   auth.Cookie `cfg:"state_cookie"`
	SuccessCookie auth.Cookie `cfg:"success_cookie"`

	// Store for effect code, only for code flow and works with redis.
	Store             store.Store `cfg:"store"`
	RedirectWhiteList []string    `cfg:"redirect_white_list"`

	client    *ok.Client `cfg:"-"`
	pathFixed PathFixed  `cfg:"-"`

	session       *session.Session  `cfg:"-"`
	store         *store.StoreCache `cfg:"-"`
	statusContent string            `cfg:"-"`
}

type Path struct {
	Base string `cfg:"base"`
	// BaseURL for adding prefix like https://example.com
	BaseURL string `cfg:"base_url"`

	Code    string `cfg:"code"`
	Token   string `cfg:"token"`
	Passkey string `cfg:"passkey"`
	InfoUI  string `cfg:"info_ui"`
	Status  string `cfg:"status"`
}

type PathFixed struct {
	Code         string
	InfoUI       string
	Token        string
	Passkey      string
	Status       string
	Signup       string
	SignupVerify string
	Reset        string
	ResetConfirm string
}

type Request struct {
	InsecureSkipVerify bool `cfg:"insecure_skip_verify"`
}

type UI struct {
	ExternalFolder bool         `cfg:"external_folder"`
	uiHandler      http.Handler `cfg:"-"`
}

func (m *Login) Init() error {
	m.session = session.GlobalRegistry.Get(m.SessionMiddleware)
	if m.session == nil {
		return errors.New("session middleware not found")
	}

	return nil
}

func (m *Login) Middleware(ctx context.Context) (func(http.Handler) http.Handler, error) {
	if m.SessionMiddleware == "" {
		return nil, fmt.Errorf("session middleware is not set")
	}

	// /////////////////////////

	embedUIFunc, err := m.SetUI()
	if err != nil {
		return nil, err
	}

	m.UI.uiHandler = embedUIFunc(nil)

	if err := m.SetFiles(); err != nil {
		return nil, err
	}

	// /////////////////////////

	// set auth client
	client, err := ok.New(
		ok.WithDisableRetry(true),
		ok.WithInsecureSkipVerify(m.Request.InsecureSkipVerify),
		ok.WithLogger(slog.Default()),
	)
	if err != nil {
		return nil, fmt.Errorf("cannot create ok client: %w", err)
	}

	m.client = client

	// path settings
	m.Path.Base = path.Join("/", strings.TrimSuffix(m.Path.Base, "/"))

	if m.Path.Code != "" {
		m.pathFixed.Code = m.Path.Code
	} else {
		m.pathFixed.Code = path.Join(m.Path.Base, "auth/code")
	}
	if m.Path.Token != "" {
		m.pathFixed.Token = m.Path.Token
	} else {
		m.pathFixed.Token = path.Join(m.Path.Base, "auth/token")
	}

	if m.Path.Passkey != "" {
		m.pathFixed.Passkey = m.Path.Passkey
	} else {
		m.pathFixed.Passkey = path.Join(m.Path.Base, "auth/passkey")
	}

	if m.Path.InfoUI != "" {
		m.pathFixed.InfoUI = m.Path.InfoUI
	} else {
		m.pathFixed.InfoUI = path.Join(m.Path.Base, "auth/info/ui")
	}

	if m.Path.Status != "" {
		m.pathFixed.Status = m.Path.Status
	} else {
		m.pathFixed.Status = path.Join(m.Path.Base, "auth/status")
	}

	// signup / password reset proxies to the auth middleware
	m.pathFixed.Signup = path.Join(m.Path.Base, "auth/signup")
	m.pathFixed.SignupVerify = path.Join(m.Path.Base, "auth/signup/verify")
	m.pathFixed.Reset = path.Join(m.Path.Base, "auth/reset")
	m.pathFixed.ResetConfirm = path.Join(m.Path.Base, "auth/reset/confirm")

	// state cookie settings
	if m.StateCookie.CookieName == "" {
		m.StateCookie.CookieName = "auth_state"
	}

	if m.StateCookie.MaxAge == 0 {
		m.StateCookie.MaxAge = 360
	}

	if m.StateCookie.Path == "" {
		m.StateCookie.Path = "/"
	}

	if m.StateCookie.SameSite == 0 {
		m.StateCookie.SameSite = http.SameSiteLaxMode
	}

	// success cookie settings
	m.SuccessCookie.CookieName = "auth_verify"

	if m.SuccessCookie.MaxAge == 0 {
		m.SuccessCookie.MaxAge = 60
	}

	if m.SuccessCookie.Path == "" {
		m.SuccessCookie.Path = "/"
	}

	if m.SuccessCookie.SameSite == 0 {
		m.SuccessCookie.SameSite = http.SameSiteLaxMode
	}

	m.SuccessCookie.HttpOnly = false

	// /////////////////////////
	storeCache, err := m.Store.Init(ctx)
	if err != nil {
		return nil, err
	}

	into.ShutdownAdd(storeCache.Close, "login-store")

	m.store = storeCache

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isLogout, _ := tcontext.Get(r, "logout").(bool); isLogout {
				m.Logout(w, r)

				return
			}

			urlPath := r.URL.Path
			method := r.Method

			switch method {
			case http.MethodGet:
				if strings.HasPrefix(urlPath, m.pathFixed.Code) {
					m.CodeFlow(w, r)

					return
				}

				if strings.HasPrefix(urlPath, m.pathFixed.InfoUI) {
					m.InformationUI(w, r)

					return
				}

				if strings.HasPrefix(urlPath, m.pathFixed.Status) {
					m.StatusHandler(w, r)

					return
				}

				customClaim, isLogged, err := m.session.IsLogged(w, r)
				if isLogged {
					if responseType := r.URL.Query().Get("response_type"); responseType == "code" {
						m.AuthCodeReturn(w, r, customClaim)

						return
					}

					m.session.RedirectToMain(w, r)

					return
				}
				if err != nil {
					_ = m.session.DelToken(w, r)
				}

				if authInfo, _ := strconv.ParseBool(r.URL.Query().Get("auth_info")); authInfo {
					m.InformationUI(w, r)

					return
				}

				m.RemoveSuccess(w)

				if m.UI.ExternalFolder {
					next.ServeHTTP(w, r)

					return
				}

				m.UIHandler(w, r)

				return
			case http.MethodPost:
				if strings.HasPrefix(urlPath, m.pathFixed.Token) {
					m.PasswordFlow(w, r)

					return
				}

				if strings.HasPrefix(urlPath, m.pathFixed.Passkey) {
					m.PasskeyFlow(w, r)

					return
				}

				// more specific prefixes first
				if strings.HasPrefix(urlPath, m.pathFixed.SignupVerify) {
					m.SignupFlow(w, r, session.SignupActionVerify)

					return
				}

				if strings.HasPrefix(urlPath, m.pathFixed.Signup) {
					m.SignupFlow(w, r, session.SignupActionSignup)

					return
				}

				if strings.HasPrefix(urlPath, m.pathFixed.ResetConfirm) {
					m.SignupFlow(w, r, session.SignupActionResetConfirm)

					return
				}

				if strings.HasPrefix(urlPath, m.pathFixed.Reset) {
					m.SignupFlow(w, r, session.SignupActionReset)

					return
				}
			}

			// not found
			writeError(w, http.StatusNotFound, http.StatusText(http.StatusNotFound))

			return
		})
	}, nil
}
