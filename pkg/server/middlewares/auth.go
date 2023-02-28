package middlewares

import (
	"context"
	"fmt"

	"github.com/labstack/echo/v4"
	"github.com/worldline-go/auth"
	"github.com/worldline-go/auth/middlewares/authecho"
	"github.com/worldline-go/turna/pkg/server/registry"
)

type Auth struct {
	Provider     auth.Provider             `cfg:"provider"`
	SkipSuffixes []string                  `cfg:"skip_suffixes"`
	Redirect     *authecho.RedirectSetting `cfg:"redirect"`
	ClaimsHeader *authecho.ClaimsHeader    `cfg:"claims_header"`
}

func (a *Auth) Middleware(ctx context.Context, name string) ([]echo.MiddlewareFunc, error) {
	activeProvider := a.Provider.ActiveProvider()
	if activeProvider == nil {
		return nil, fmt.Errorf("not found an active authentication provider")
	}

	options := []authecho.Option{}

	if introspectURL := activeProvider.GetIntrospectURL(); introspectURL != "" {
		v, _ := activeProvider.JWTKeyFunc(auth.WithContext(ctx), auth.WithIntrospect(true))
		options = append(options, authecho.WithKeyFunc(v.Keyfunc), authecho.WithParserFunc(v.Parser))
	} else {
		jwks, err := activeProvider.JWTKeyFunc(auth.WithContext(ctx))
		if err != nil {
			return nil, err
		}

		registry.GlobalReg.AddShutdownFunc(name, jwks.EndBackground)
		options = append(options, authecho.WithKeyFunc(jwks.Keyfunc))
	}

	if len(a.SkipSuffixes) > 0 {
		options = append(options, authecho.WithSkipper(
			authecho.NewSkipper(
				authecho.WithSuffixes(a.SkipSuffixes...),
			),
		))
	}

	if a.ClaimsHeader != nil {
		options = append(options, authecho.WithClaimsHeader(a.ClaimsHeader))
	}

	if a.Redirect != nil {
		authURL := activeProvider.GetAuthURL()
		if authURL == "" {
			return nil, fmt.Errorf("auth url is empty")
		}

		tokenUrl := activeProvider.GetTokenURL()
		if tokenUrl == "" {
			return nil, fmt.Errorf("token url is empty")
		}

		a.Redirect.AuthURL = authURL
		a.Redirect.TokenURL = tokenUrl
		a.Redirect.ClientID = activeProvider.GetClientID()
		a.Redirect.ClientSecret = activeProvider.GetClientSecret()
		a.Redirect.Scopes = activeProvider.GetScopes()

		options = append(options, authecho.WithRedirect(a.Redirect))
	}

	return authecho.MiddlewareJWTWithRedirection(options...), nil
}
