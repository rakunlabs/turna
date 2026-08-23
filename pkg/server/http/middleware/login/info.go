package login

import (
	"net/http"
	"path"
	"sort"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
)

type Info struct {
	Title string `cfg:"title"`
	// DisableRememberMe hides the remember-me choice on the embedded login
	// page; every sign-in then proceeds as a standard (non-remembered) session.
	DisableRememberMe bool `cfg:"disable_remember_me"`
}

type InfoUIResponse struct {
	Title             string       `json:"title"`
	DisableRememberMe bool         `json:"disable_remember_me,omitempty"`
	Provider          InfoProvider `json:"provider"`
	Error             string       `json:"error,omitempty"`
}

type MethodsResponse struct {
	Payload InfoUIResponse `json:"payload"`
}

type InfoProvider struct {
	Password []Link `json:"password"`
	Code     []Link `json:"code"`
	Passkey  []Link `json:"passkey"`
}

type Link struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	Priority int    `json:"-"`

	// optional signup / forgot-password proxy endpoints; only set on
	// password providers whose auth middleware enables those flows.
	SignupURL               string `json:"signup_url,omitempty"`
	SignupVerifyURL         string `json:"signup_verify_url,omitempty"`
	PasswordResetURL        string `json:"password_reset_url,omitempty"`
	PasswordResetConfirmURL string `json:"password_reset_confirm_url,omitempty"`
	// PasswordMinLength is advertised so the signup/reset forms can enforce
	// and display the configured minimum; 0 means the UI default applies.
	PasswordMinLength int `json:"password_min_length,omitempty"`
}

func (i Info) value() Info {
	if i.Title == "" {
		i.Title = "Login"
	}

	return i
}

// rememberMe applies the disable_remember_me switch to a requested value, so a
// hand-crafted remember_me=true cannot bypass the hidden checkbox.
func (m *Login) rememberMe(requested bool) bool {
	if m.Info.DisableRememberMe {
		return false
	}

	return requested
}

func (m *Login) informationUIResponse() InfoUIResponse {
	info := m.Info.value()

	response := InfoUIResponse{
		Title:             info.Title,
		DisableRememberMe: info.DisableRememberMe,
	}

	for providerName, provider := range m.session.Providers() {
		if provider.Hide {
			continue
		}

		oauth2 := provider.Oauth2
		if oauth2 == nil {
			continue
		}

		name := providerName
		if provider.Name != "" {
			name = provider.Name
		}

		if provider.Passkey && (provider.AuthMiddleware != "" || oauth2.PasskeyURL != "") {
			response.Provider.Passkey = append(response.Provider.Passkey, Link{
				Name:     name,
				URL:      m.Path.BaseURL + path.Join(m.pathFixed.Passkey, providerName),
				Priority: provider.Priority,
			})
		}

		if provider.PasswordFlow {
			link := Link{
				Name:     name,
				URL:      m.Path.BaseURL + path.Join(m.pathFixed.Token, providerName),
				Priority: provider.Priority,
			}

			// advertise signup/forgot-password when the auth middleware
			// enables them; checked live so UI toggles apply immediately.
			features, _ := providerSignup(provider)
			link.PasswordMinLength = features.PasswordMinLength
			if features.Signup {
				link.SignupURL = m.Path.BaseURL + path.Join(m.pathFixed.Signup, providerName)
				link.SignupVerifyURL = m.Path.BaseURL + path.Join(m.pathFixed.SignupVerify, providerName)
			}
			if features.PasswordReset {
				link.PasswordResetURL = m.Path.BaseURL + path.Join(m.pathFixed.Reset, providerName)
				link.PasswordResetConfirmURL = m.Path.BaseURL + path.Join(m.pathFixed.ResetConfirm, providerName)
			}

			response.Provider.Password = append(response.Provider.Password, link)

			continue
		}

		response.Provider.Code = append(response.Provider.Code, Link{
			Name:     name,
			URL:      m.Path.BaseURL + path.Join(m.pathFixed.Code, providerName),
			Priority: provider.Priority,
		})
	}

	// sort by priority
	sort.Slice(response.Provider.Code, func(i, j int) bool {
		return response.Provider.Code[i].Priority < response.Provider.Code[j].Priority
	})

	sort.Slice(response.Provider.Password, func(i, j int) bool {
		return response.Provider.Password[i].Priority < response.Provider.Password[j].Priority
	})

	sort.Slice(response.Provider.Passkey, func(i, j int) bool {
		return response.Provider.Passkey[i].Priority < response.Provider.Passkey[j].Priority
	})

	return response
}

// Methods returns the canonical login method manifest in the standard API
// payload envelope.
func (m *Login) Methods(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	httputil.JSON(w, http.StatusOK, MethodsResponse{Payload: m.informationUIResponse()})
}

// InformationUI keeps the historic unwrapped response shape for the
// /auth/info/ui and ?auth_info=true compatibility aliases.
func (m *Login) InformationUI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	httputil.JSON(w, http.StatusOK, m.informationUIResponse())
}
