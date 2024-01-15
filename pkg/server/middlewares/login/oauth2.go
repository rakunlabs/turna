package login

import (
	"github.com/worldline-go/auth/request"
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
}

func (o *Oauth2) PasswordToken(username, password string) request.PassswordConfig {
	return request.PassswordConfig{
		Username: username,
		Password: password,
		Scopes:   o.Scopes,
		AuthRequestConfig: request.AuthRequestConfig{
			ClientID:     o.ClientID,
			ClientSecret: o.ClientSecret,
			TokenURL:     o.TokenURL,
			Scopes:       o.Scopes,
		},
	}
}
