package session

type MetaData struct {
	Error string `json:"error"`
}

// AuthHeaderStyle is a type to set Authorization header style.
type AuthHeaderStyle int

const (
	AuthHeaderStyleBasic AuthHeaderStyle = iota
	AuthHeaderStyleBearerSecret
	AuthHeaderStyleParams
)

type Oauth2 struct {
	// ClientID is the application's ID.
	ClientID string `cfg:"client_id" json:"client_id"`
	// ClientSecret is the application's secret.
	ClientSecret string `cfg:"client_secret" json:"client_secret" log:"false"`
	// Scope specifies optional requested permissions.
	Scopes []string `cfg:"scopes" json:"scopes,omitempty"`
	// CertURL is the resource server's public key URL.
	CertURL string `cfg:"cert_url" json:"cert_url,omitempty"`
	// IntrospectURL is the check the active or not with request.
	IntrospectURL string `cfg:"introspect_url" json:"introspect_url,omitempty"`
	// UserInfoURL is the get information about user.
	UserInfoURL string `cfg:"userinfo_url" json:"userinfo_url,omitempty"`
	// RevocationURL for token revocation.
	RevocationURL string `cfg:"revocation_url" json:"revocation_url,omitempty"`
	// AuthURL is the resource server's authorization endpoint
	// use for redirection to login page.
	AuthURL string `cfg:"auth_url" json:"auth_url,omitempty"`
	// TokenURL is the resource server's token endpoint URL.
	TokenURL  string `cfg:"token_url" json:"token_url,omitempty"`
	LogoutURL string `cfg:"logout_url" json:"logout_url,omitempty"`
	// PasskeyURL is the WebAuthn begin/finish endpoint of a remote auth
	// middleware (e.g. https://auth.example.com/auth/oauth2/passkey).
	// Not needed when the provider uses auth_middleware (in-process).
	PasskeyURL string `cfg:"passkey_url" json:"passkey_url,omitempty"`
	// APIKeyURL is the static API key validation endpoint of a remote auth
	// middleware (e.g. https://auth.example.com/auth/oauth2/api-key).
	// Not needed when the provider uses auth_middleware (in-process).
	APIKeyURL string `cfg:"api_key_url" json:"api_key_url,omitempty"`
	// SignupURL is the self-registration endpoint of a remote auth middleware
	// (e.g. https://auth.example.com/auth/oauth2/signup); the verify endpoint
	// is derived as SignupURL + "/verify".
	// Not needed when the provider uses auth_middleware (in-process).
	SignupURL string `cfg:"signup_url" json:"signup_url,omitempty"`
	// PasswordResetURL is the forgot-password endpoint of a remote auth
	// middleware (e.g. https://auth.example.com/auth/oauth2/password-reset);
	// the confirm endpoint is derived as PasswordResetURL + "/confirm".
	// Not needed when the provider uses auth_middleware (in-process).
	PasswordResetURL string `cfg:"password_reset_url" json:"password_reset_url,omitempty"`
	// AuthHeaderStyle is optional. If not set, AuthHeaderStyleBasic will be used.
	AuthHeaderStyle AuthHeaderStyle `cfg:"auth_header_style" json:"auth_header_style,omitempty"`
}

type TokenData struct {
	AccessToken      string `json:"access_token"`
	ExpiresIn        int    `json:"expires_in"`
	RefreshExpiresIn int    `json:"refresh_expires_in"`
	RefreshToken     string `json:"refresh_token"`
	TokenType        string `json:"token_type"`
	NotBeforePolicy  int    `json:"not-before-policy"`
	SessionState     string `json:"session_state"`
	Scope            string `json:"scope"`
	IDToken          string `json:"id_token"`
}
