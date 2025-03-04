package oauth2

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
	ExpiresIn             int64  `json:"expires_in"`
	RefreshToken          string `json:"refresh_token"`
	RefreshTokenExpiresIn int64  `json:"refresh_token_expires_in"`
	Scope                 string `json:"scope,omitempty"`
	IDToken               string `json:"id_token,omitempty"`
}
