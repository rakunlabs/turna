package middlewares

import (
	"context"

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
	jwks, err := a.Provider.GetJwks(ctx)
	if err != nil {
		return nil, err
	}

	registry.GlobalReg.AddShutdownFunc(name, jwks.EndBackground)

	options := []authecho.Option{authecho.WithKeyFunc(jwks.Keyfunc)}

	if len(a.SkipSuffixes) > 0 {
		options = append(options, authecho.WithSkipper(authecho.NewSkipper(a.SkipSuffixes...)))
	}

	if a.ClaimsHeader != nil {
		options = append(options, authecho.WithClaimsHeader(a.ClaimsHeader))
	}

	if a.Redirect != nil {
		activeProvider, err := a.Provider.ActiveProvider()
		if err != nil {
			return nil, err
		}

		authURL, err := activeProvider.GetAuthURL()
		if err != nil {
			return nil, err
		}

		tokenUrl, err := activeProvider.GetTokenURL()
		if err != nil {
			return nil, err
		}

		a.Redirect.AuthURL = authURL
		a.Redirect.TokenURL = tokenUrl
		a.Redirect.ClientID = activeProvider.GetClientID()
		a.Redirect.ClientSecret = activeProvider.GetClientSecret()

		options = append(options, authecho.WithRedirect(a.Redirect))
	}

	return authecho.MiddlewareJWTWithRedirection(options...), nil
}
