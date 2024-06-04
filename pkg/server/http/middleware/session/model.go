package session

import (
	"encoding/base64"
	"encoding/json"
)

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
	// AuthURL is the resource server's authorization endpoint
	// use for redirection to login page.
	AuthURL string `cfg:"auth_url"`
	// TokenURL is the resource server's token endpoint URL.
	TokenURL  string `cfg:"token_url"`
	LogoutURL string `cfg:"logout_url"`
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

// Parse64 parse the cookie
func ParseToken64(v string) (*TokenData, error) {
	vByte := []byte(v)

	vByte, err := base64.StdEncoding.DecodeString(v)
	if err != nil {
		return nil, err
	}

	tokenData := TokenData{}
	if err := json.Unmarshal(vByte, &tokenData); err != nil {
		return nil, err
	}

	return &tokenData, nil
}

func ParseToken(v []byte) (*TokenData, error) {
	tokenData := TokenData{}

	if err := json.Unmarshal(v, &tokenData); err != nil {
		return nil, err
	}

	return &tokenData, nil
}
