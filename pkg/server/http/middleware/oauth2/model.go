package oauth2

import (
	"encoding/base64"
	"encoding/json"
)

type AccessTokenRequest struct {
	GrantType string `form:"grant_type"`
	Scope     string `form:"scope,omitempty"`

	RefreshToken string `form:"refresh_token,omitempty"`

	Username string `form:"username,omitempty"`
	Password string `form:"password,omitempty"`

	RedirectURI string `form:"redirect_uri,omitempty"`
	Code        string `form:"code,omitempty"`
}

type AccessTokenErrorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`

	code int `json:"-"`
}

func (e AccessTokenErrorResponse) GetCode() int {
	return e.code
}

type AccessTokenResponse struct {
	TokenType             string `json:"token_type"`
	AccessToken           string `json:"access_token"`
	ExpiresIn             int    `json:"expires_in"`
	RefreshToken          string `json:"refresh_token"`
	RefreshTokenExpiresIn int    `json:"refresh_token_expires_in"`
	Scope                 string `json:"scope,omitempty"`
}

type State struct {
	RedirectURI string `json:"redirect_uri"`
	State       string `json:"state"`
	OrgState    string `json:"org_state"`
}

func EncodeState(state State) (string, error) {
	v, err := json.Marshal(state)
	v64 := base64.StdEncoding.EncodeToString(v)

	return v64, err
}

func DecodeState(encoded string) (State, error) {
	var state State

	v, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return state, err
	}

	err = json.Unmarshal(v, &state)

	return state, err
}
