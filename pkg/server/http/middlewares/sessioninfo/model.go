package sessioninfo

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/worldline-go/auth/claims"

	"github.com/rakunlabs/turna/pkg/server/http/middlewares/session"
	"github.com/rakunlabs/turna/pkg/server/model"
)

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

func (m *Info) Info(c echo.Context) error {
	// get session middleware
	sessionM := session.GlobalRegistry.Get(m.SessionMiddleware)
	if sessionM == nil {
		return c.JSON(http.StatusInternalServerError, model.MetaData{Error: "session middleware not found"})
	}

	// check if token exist in store
	v64 := ""
	if v, err := sessionM.GetStore().Get(c.Request(), sessionM.GetCookieName(c)); !v.IsNew && err == nil {
		// add the access token to the request
		v64, _ = v.Values[session.TokenKey].(string)
	} else {
		if err != nil {
			return c.JSON(http.StatusInternalServerError, model.MetaData{Error: err.Error()})
		}

		// cookie not found
		return c.JSON(http.StatusNotFound, model.MetaData{Error: "cookie not found"})
	}

	// check if token is valid
	token, err := session.ParseToken64(v64)
	if err != nil {
		return c.JSON(http.StatusForbidden, model.MetaData{Error: fmt.Sprintf("cannot parse token: %v", err)})
	}

	// check if token is valid
	claim := claims.Custom{}
	if _, err := sessionM.Action.Token.GetKeyFunc().ParseWithClaims(token.AccessToken, &claim); err != nil {
		return c.JSON(http.StatusForbidden, model.MetaData{Error: err.Error()})
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

	return c.JSON(http.StatusOK, response)
}
