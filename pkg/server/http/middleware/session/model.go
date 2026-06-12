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
	ClientID string `cfg:"client_id"`
	// ClientSecret is the application's secret.
	ClientSecret string `cfg:"client_secret" log:"false"`
	// Scope specifies optional requested permissions.
	Scopes []string `cfg:"scopes"`
	// CertURL is the resource server's public key URL.
	CertURL string `cfg:"cert_url"`
	// IntrospectURL is the check the active or not with request.
	IntrospectURL string `cfg:"introspect_url"`
	// UserInfoURL is the get information about user.
	UserInfoURL string `cfg:"userinfo_url"`
	// RevocationURL for token revocation.
	RevocationURL string `cfg:"revocation_url"`
	// AuthURL is the resource server's authorization endpoint
	// use for redirection to login page.
	AuthURL string `cfg:"auth_url"`
	// TokenURL is the resource server's token endpoint URL.
	TokenURL  string `cfg:"token_url"`
	LogoutURL string `cfg:"logout_url"`
	// PasskeyURL is the WebAuthn begin/finish endpoint of a remote auth
	// middleware (e.g. https://auth.example.com/auth/oauth2/passkey).
	// Not needed when the provider uses auth_middleware (in-process).
	PasskeyURL string `cfg:"passkey_url"`
	// APIKeyURL is the static API key validation endpoint of a remote auth
	// middleware (e.g. https://auth.example.com/auth/oauth2/api-key).
	// Not needed when the provider uses auth_middleware (in-process).
	APIKeyURL string `cfg:"api_key_url"`
	// SignupURL is the self-registration endpoint of a remote auth middleware
	// (e.g. https://auth.example.com/auth/oauth2/signup); the verify endpoint
	// is derived as SignupURL + "/verify".
	// Not needed when the provider uses auth_middleware (in-process).
	SignupURL string `cfg:"signup_url"`
	// PasswordResetURL is the forgot-password endpoint of a remote auth
	// middleware (e.g. https://auth.example.com/auth/oauth2/password-reset);
	// the confirm endpoint is derived as PasswordResetURL + "/confirm".
	// Not needed when the provider uses auth_middleware (in-process).
	PasswordResetURL string `cfg:"password_reset_url"`
	// AuthHeaderStyle is optional. If not set, AuthHeaderStyleBasic will be used.
	AuthHeaderStyle AuthHeaderStyle
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
