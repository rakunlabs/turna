package session

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/worldline-go/auth/claims"
	storeAuth "github.com/worldline-go/auth/store"
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

func (m *Session) Info(c echo.Context) error {
	// check if token exist in store
	v64 := ""
	if v, err := m.store.Get(c.Request(), m.CookieName); !v.IsNew && err == nil {
		// add the access token to the request
		v64, _ = v.Values[m.ValueName].(string)
	} else {
		if err != nil {
			return c.JSON(http.StatusInternalServerError, MetaData{Error: err.Error()})
		}

		// cookie not found
		return c.JSON(http.StatusNotFound, MetaData{Error: "cookie not found"})
	}

	// check if token is valid
	token, err := storeAuth.Parse(v64, storeAuth.WithBase64(true))
	if err != nil {
		c.Logger().Errorf("cannot parse token: %v", err)
		return m.RedirectToLogin(c, m.store, true, true)
	}

	// check if token is valid
	claim := claims.Custom{}
	if _, err := m.Actions.Token.keyFunc.ParseWithClaims(token.AccessToken, &claim); err != nil {
		return c.JSON(http.StatusForbidden, MetaData{Error: err.Error()})
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
		response["roles"] = claim.Roles
	}

	if m.Information.Scopes {
		response["scopes"] = strings.Fields(claim.Scope)
	}

	return c.JSON(http.StatusOK, response)
}
