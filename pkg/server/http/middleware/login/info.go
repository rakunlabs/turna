package login

import (
	"net/http"
	"path"
	"sort"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
)

type Info struct {
	Title string `cfg:"title"`
}

type InfoUIResponse struct {
	Title    string       `json:"title"`
	Provider InfoProvider `json:"provider"`
	Error    string       `json:"error,omitempty"`
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

func (m *Login) InformationUI(w http.ResponseWriter, r *http.Request) {
	info := m.Info.value()

	response := InfoUIResponse{
		Title: info.Title,
	}

	for providerName := range m.session.Provider {
		if m.session.Provider[providerName].Hide {
			continue
		}

		oauth2 := m.session.Provider[providerName].Oauth2
		if oauth2 == nil {
			continue
		}

		name := providerName
		if m.session.Provider[providerName].Name != "" {
			name = m.session.Provider[providerName].Name
		}

		if m.session.Provider[providerName].Passkey && (m.session.Provider[providerName].AuthMiddleware != "" || oauth2.PasskeyURL != "") {
			response.Provider.Passkey = append(response.Provider.Passkey, Link{
				Name:     name,
				URL:      m.Path.BaseURL + path.Join(m.pathFixed.Passkey, providerName),
				Priority: m.session.Provider[providerName].Priority,
			})
		}

		if m.session.Provider[providerName].PasswordFlow {
			link := Link{
				Name:     name,
				URL:      m.Path.BaseURL + path.Join(m.pathFixed.Token, providerName),
				Priority: m.session.Provider[providerName].Priority,
			}

			// advertise signup/forgot-password when the auth middleware
			// enables them; checked live so UI toggles apply immediately.
			features, _ := providerSignup(m.session.Provider[providerName])
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
			Priority: m.session.Provider[providerName].Priority,
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

	httputil.JSON(w, http.StatusOK, response)
}
