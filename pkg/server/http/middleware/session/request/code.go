package request

import (
	"context"
	"net/url"
)

type AuthorizationCodeConfig struct {
	Code        string
	RedirectURL string

	// EndpointParams specifies additional parameters for requests to the token endpoint.
	EndpointParams url.Values

	AuthRequestConfig
}

// AuthorizationCode is a function to handle authorization code flow.
//
// Returns a byte array of the response body, if the response status code is 2xx.
func (a *Auth) AuthorizationCode(ctx context.Context, cfg AuthorizationCodeConfig) ([]byte, error) {
	uValues := url.Values{
		"grant_type": {"authorization_code"},
		"code":       {cfg.Code},
	}

	if cfg.RedirectURL != "" {
		uValues.Set("redirect_uri", cfg.RedirectURL)
	}

	for k, p := range cfg.EndpointParams {
		uValues[k] = p
	}

	return a.AuthRequest(ctx, uValues, cfg.AuthRequestConfig)
}
