package login

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/worldline-go/turna/pkg/server/http/httputil"
	"github.com/worldline-go/turna/pkg/server/http/middleware/oauth2/auth"
	"github.com/worldline-go/turna/pkg/server/http/middleware/session"
	"github.com/worldline-go/turna/pkg/server/model"
)

type TokenRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (m *Login) CodeFlowInit(w http.ResponseWriter, r *http.Request, providerName string) {
	m.RemoveSuccess(w)

	state, err := auth.NewState()
	if err != nil {
		httputil.JSON(w, http.StatusInternalServerError, model.MetaData{Message: err.Error()})

		return
	}

	m.SetState(w, state)

	sessionM := session.GlobalRegistry.Get(m.SessionMiddleware)
	if sessionM == nil {
		httputil.JSON(w, http.StatusInternalServerError, model.MetaData{Message: "session middleware not found"})

		return
	}

	provider := sessionM.Provider[providerName]
	if provider.Oauth2 == nil {
		httputil.JSON(w, http.StatusNotFound, model.MetaData{Message: fmt.Sprintf("provider %q not found", providerName)})

		return
	}

	authCodeURL, err := m.AuthCodeURL(r, state, providerName, provider.Oauth2)
	if err != nil {
		httputil.JSON(w, http.StatusInternalServerError, model.MetaData{Message: err.Error()})

		return
	}

	httputil.Redirect(w, http.StatusTemporaryRedirect, authCodeURL)
}

func (m *Login) CodeFlow(w http.ResponseWriter, r *http.Request) {
	pathSplit := strings.Split(r.URL.Path, "/")
	providerName := pathSplit[len(pathSplit)-1]

	sessionM := session.GlobalRegistry.Get(m.SessionMiddleware)
	if sessionM == nil {
		httputil.JSON(w, http.StatusInternalServerError, model.MetaData{Message: "session middleware not found"})

		return
	}

	var oauth2 *session.Oauth2
	if v, ok := sessionM.Provider[providerName]; ok && v.Oauth2 != nil {
		oauth2 = v.Oauth2
	}

	if oauth2 == nil {
		httputil.JSON(w, http.StatusNotFound, model.MetaData{Message: fmt.Sprintf("provider %q not found", providerName)})

		return
	}

	// get the token from the request
	query := r.URL.Query()
	code := query.Get("code")
	if code == "" {
		m.CodeFlowInit(w, r, providerName)

		return
	}

	state := query.Get("state")
	// check state
	if m.CheckState(w, r, state) != nil {
		httputil.JSON(w, http.StatusUnauthorized, model.MetaData{Message: "state is not valid"})

		return
	}

	// get token from provider
	data, statusCode, err := m.CodeToken(r, code, providerName, oauth2)
	if err != nil {
		httputil.JSON(w, statusCode, model.MetaData{Message: err.Error()})

		return
	}

	if err := sessionM.SetToken(w, r, data, providerName); err != nil {
		httputil.JSON(w, http.StatusInternalServerError, model.MetaData{Message: err.Error()})

		return
	}

	// pop-up close window
	m.SetSuccess(w, "true")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte("<script>window.close();</script>"))
}

func (m *Login) PasswordFlow(w http.ResponseWriter, r *http.Request) {
	pathSplit := strings.Split(r.URL.Path, "/")
	providerName := pathSplit[len(pathSplit)-1]

	// save token in session
	sessionM := session.GlobalRegistry.Get(m.SessionMiddleware)
	if sessionM == nil {
		httputil.JSON(w, http.StatusInternalServerError, model.MetaData{Message: "session middleware not found"})

		return
	}

	var oauth2 *session.Oauth2
	if v, ok := sessionM.Provider[providerName]; ok && v.Oauth2 != nil {
		oauth2 = v.Oauth2
	}

	if oauth2 == nil {
		httputil.JSON(w, http.StatusNotFound, model.MetaData{Message: fmt.Sprintf("provider %q not found", providerName)})

		return
	}

	var request TokenRequest

	if err := httputil.DecodeJSON(r.Body, &request); err != nil {
		httputil.JSON(w, http.StatusBadRequest, model.MetaData{Message: err.Error()})

		return
	}

	// get Token from provider
	data, statusCode, err := m.PasswordToken(r.Context(), request.Username, request.Password, oauth2)
	if err != nil {
		httputil.JSON(w, statusCode, model.MetaData{Message: err.Error()})

		return
	}

	if err := sessionM.SetToken(w, r, data, providerName); err != nil {
		httputil.JSON(w, http.StatusInternalServerError, model.MetaData{Message: err.Error()})

		return
	}

	httputil.NoContent(w, http.StatusNoContent)
}
