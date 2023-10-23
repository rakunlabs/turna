package middlewares

import (
	"context"
	"fmt"

	"github.com/labstack/echo/v4"
	"github.com/worldline-go/auth"
	"github.com/worldline-go/auth/pkg/authecho"
	"github.com/worldline-go/auth/redirect"
)

type Auth struct {
	Provider     auth.Provider     `cfg:"provider"`
	SkipSuffixes []string          `cfg:"skip_suffixes"`
	Redirect     *redirect.Setting `cfg:"redirect"`
}

func (a *Auth) Middleware(ctx context.Context, name string) ([]echo.MiddlewareFunc, error) {
	activeProvider := a.Provider.ActiveProvider()
	if activeProvider == nil {
		return nil, fmt.Errorf("not found an active authentication provider")
	}

	options := []authecho.Option{}

	if introspectURL := activeProvider.GetIntrospectURL(); introspectURL != "" {
		provideJwks, err := activeProvider.JWTKeyFunc(auth.WithContext(ctx), auth.WithIntrospect(true))
		if err != nil {
			return nil, fmt.Errorf("failed to create JWTKeyFunc with introspect: %w", err)
		}

		options = append(options, authecho.WithKeyFunc(provideJwks.Keyfunc), authecho.WithParserFunc(provideJwks.ParseWithClaims))
	} else {
		provideJwks, err := activeProvider.JWTKeyFunc(auth.WithContext(ctx))
		if err != nil {
			return nil, fmt.Errorf("failed to create JWTKeyFunc: %w", err)
		}

		options = append(options, authecho.WithKeyFunc(provideJwks.Keyfunc))
	}

	if len(a.SkipSuffixes) > 0 {
		options = append(options, authecho.WithSkipper(
			authecho.NewSkipper(
				authecho.WithSuffixes(a.SkipSuffixes...),
			),
		))
	}

	if a.Redirect != nil {
		authURL := activeProvider.GetAuthURL()
		if authURL == "" {
			return nil, fmt.Errorf("auth url is empty")
		}

		tokenURL := activeProvider.GetTokenURL()
		if tokenURL == "" {
			return nil, fmt.Errorf("token url is empty")
		}

		a.Redirect.AuthURL = authURL
		a.Redirect.TokenURL = tokenURL
		a.Redirect.ClientID = activeProvider.GetClientID()
		a.Redirect.ClientSecret = activeProvider.GetClientSecret()
		a.Redirect.Scopes = activeProvider.GetScopes()
		a.Redirect.LogoutURL = activeProvider.GetLogoutURL()

		options = append(options, authecho.WithRedirect(a.Redirect))
	}

	return authecho.MiddlewareJWTWithRedirection(options...), nil
}
