package sessioninfo

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/worldline-go/turna/pkg/server/http/httputil"
	"github.com/worldline-go/turna/pkg/server/http/middleware/oauth2/claims"
	"github.com/worldline-go/turna/pkg/server/http/middleware/session"
	"github.com/worldline-go/turna/pkg/server/model"
)

type Info struct {
	Information       Information `cfg:"information"`
	SessionMiddleware string      `cfg:"session_middleware"`
}

type Information struct {
	// Values list to store in the cookie like "preferred_username", "given_name", "family_name", "sid", "azp", "aud"
	Values []string `cfg:"values"`
	// Custom map to store in the cookie.
	Custom map[string]interface{} `cfg:"custom"`
	// Roles to store in the cookie as []string.
	Roles bool `cfg:"roles"`
	// Scopes to store in the cookie as []string.
	Scopes bool `cfg:"scopes"`
}

func (m *Info) Middleware() (func(http.Handler) http.Handler, error) {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(m.Info)
	}, nil
}

func (m *Info) Info(w http.ResponseWriter, r *http.Request) {
	// get session middleware
	sessionM := session.GlobalRegistry.Get(m.SessionMiddleware)
	if sessionM == nil {
		httputil.JSON(w, http.StatusInternalServerError, model.MetaData{Message: "session middleware not found"})

		return
	}

	// check if token exist in store
	v64 := ""
	if v, err := sessionM.GetStore().Get(r, sessionM.GetCookieName(r)); !v.IsNew && err == nil {
		// add the access token to the request
		v64, _ = v.Values[session.TokenKey].(string)
	} else {
		if err != nil {
			httputil.JSON(w, http.StatusInternalServerError, model.MetaData{Message: err.Error()})

			return
		}

		// cookie not found
		httputil.JSON(w, http.StatusNotFound, model.MetaData{Message: "cookie not found"})

		return
	}

	// check if token is valid
	token, err := session.ParseToken64(v64)
	if err != nil {
		httputil.JSON(w, http.StatusForbidden, model.MetaData{Message: fmt.Sprintf("cannot parse token: %v", err)})

		return
	}

	// check if token is valid
	claim := claims.Custom{}
	if _, err := sessionM.Action.Token.GetKeyFunc().ParseWithClaims(token.AccessToken, &claim); err != nil {
		httputil.JSON(w, http.StatusForbidden, model.MetaData{Message: err.Error()})

		return
	}

	// return the claims
	totalLen := len(m.Information.Values) + len(m.Information.Custom)
	if m.Information.Roles {
		totalLen++
	}
	if m.Information.Scopes {
		totalLen++
	}

	response := make(map[string]interface{}, totalLen)

	for _, v := range m.Information.Values {
		if claim, ok := claim.Map[v]; ok {
			response[v] = claim
		}
	}

	for k, v := range m.Information.Custom {
		response[k] = v
	}

	if m.Information.Roles {
		roles := make([]string, 0, len(claim.RoleSet))
		for role := range claim.RoleSet {
			roles = append(roles, role)
		}

		response["roles"] = roles
	}

	if m.Information.Scopes {
		response["scopes"] = strings.Fields(claim.Scope)
	}

	httputil.JSON(w, http.StatusOK, response)
}
