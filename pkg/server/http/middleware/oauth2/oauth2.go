package oauth2

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/rakunlabs/into"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/oauth2/auth"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/oauth2/store"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/oauth2/token"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/session"
)

type Oauth2 struct {
	PrefixPath    string `cfg:"prefix_path"`
	Token         Token  `cfg:"token"`
	IamMiddleware string `cfg:"iam_middleware"`

	AccessClients map[string]AccessClient `cfg:"access_clients"`

	// Providers for get token from there but return token from here.
	Providers map[string]*session.Oauth2 `cfg:"providers"`
	// Code for code auth flow.
	Code auth.Code `cfg:"code"`
	// Store for cache temporary data.
	Store store.Store `cfg:"store"`
	// PassLower for pass lower case on password flow.
	PassLower bool `cfg:"pass_lower"`

	WellKnown map[string]map[string]any `cfg:"well_known"`

	storeCache *store.StoreCache
	jwt        *token.JWT
	iam        *iam.Iam
}

type AccessClient struct {
	ClientSecret  string   `cfg:"client_secret"`
	Scope         []string `cfg:"scope"`
	WhitelistURLs []string `cfg:"whitelist_urls"`
}

func (m *Oauth2) Middleware(ctx context.Context) (func(http.Handler) http.Handler, error) {
	if err := m.Code.Init(); err != nil {
		return nil, err
	}

	// /////////////////////////
	storeCache, err := m.Store.Init(ctx)
	if err != nil {
		return nil, err
	}

	into.ShutdownAdd(storeCache.Close, "oauth2-store")

	m.storeCache = storeCache

	// /////////////////////////

	rsaPrivateKeyRaw, err := m.Token.Cert.RSA.GetPrivateKey()
	if err != nil {
		return nil, err
	}

	rsaPublicKeyRaw, err := m.Token.Cert.RSA.GetPublicKey()
	if err != nil {
		return nil, err
	}

	rsaPrivateKey, err := token.ParseRSAPrivateKey(rsaPrivateKeyRaw)
	if err != nil {
		return nil, err
	}

	rsaPublicKey, err := token.ParseRSAPublicKey(rsaPublicKeyRaw)
	if err != nil {
		return nil, err
	}

	m.Token.Cert.RSA.private = rsaPrivateKey
	m.Token.Cert.RSA.public = rsaPublicKey

	jwt, err := token.NewJWT(
		token.WithKID(m.Token.KID),
		token.WithMethod(jwt.SigningMethodRS256),
		token.WithRSAPrivateKey(m.Token.Cert.RSA.private),
		token.WithRSAPublicKey(m.Token.Cert.RSA.public),
	)
	if err != nil {
		return nil, err
	}

	m.jwt = jwt

	if m.Token.TokenLifetime == 0 {
		m.Token.TokenLifetime = DefaultTokenLifetime
	}

	if m.Token.RefreshLifetime == 0 {
		m.Token.RefreshLifetime = DefaultRefreshLifetime
	}

	// /////////////////////////

	m.PrefixPath = "/" + strings.Trim(m.PrefixPath, "/")

	mux := m.MuxSet(m.PrefixPath)

	return func(next http.Handler) http.Handler {
		mux.NotFound(next.ServeHTTP)

		return mux
	}, nil
}

func (m *Oauth2) Init() error {
	m.iam = iam.GlobalRegistry.Get(m.IamMiddleware)
	if m.iam == nil {
		return errors.New("iam middleware not found")
	}

	return nil
}
