package session

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/MicahParks/keyfunc/v2"
	"github.com/golang-jwt/jwt/v5"
)

var (
	ErrKIDNotFound  = keyfunc.ErrKIDNotFound
	ErrTokenInvalid = fmt.Errorf("token is invalid")
)

type InfProviderCert interface {
	GetCertURL() string
	GetName() string
}

type InfKeyFunc interface {
	Keyfunc(token *jwt.Token) (interface{}, error)
}

type InfKeyFuncParser interface {
	InfKeyFunc
	ParseWithClaims(tokenString string, claims jwt.Claims) (*jwt.Token, error)
}

type JwkKeyFuncParse struct {
	KeyFunc func(token *jwt.Token) (interface{}, error)
}

func (j *JwkKeyFuncParse) Keyfunc(token *jwt.Token) (interface{}, error) {
	if j.KeyFunc != nil {
		return j.KeyFunc(token)
	}

	return nil, fmt.Errorf("not implemented")
}

func (j *JwkKeyFuncParse) ParseWithClaims(tokenString string, claims jwt.Claims) (*jwt.Token, error) {
	// Parse the JWT.
	token, err := jwt.ParseWithClaims(tokenString, claims, j.Keyfunc)
	if err != nil {
		return nil, fmt.Errorf("failed to parse the JWT: %w", err)
	}

	// Check if the token is valid.
	if !token.Valid {
		return nil, ErrTokenInvalid
	}

	return token, nil
}

type KeyFuncMulti struct {
	givenJwks InfKeyFunc
	multiJWKS *MultipleJWKS
}

func (k *KeyFuncMulti) KeySelectorFirst(multiJWKS *MultipleJWKS, token *jwt.Token) (interface{}, error) {
	if k.givenJwks != nil {
		key, err := k.givenJwks.Keyfunc(token)
		if err == nil {
			return key, nil
		}
		if !errors.Is(err, ErrKIDNotFound) {
			return nil, err
		}
	}

	v, err := KeySelectorFirst(multiJWKS, token)
	if err != nil {
		return nil, err
	}

	token.Header["provider_name"] = v.Name

	return v.Key, nil
}

// KeySelectorFirst returns the first key found in the multiple JWK Sets.
func KeySelectorFirst(multiJWKS *MultipleJWKS, token *jwt.Token) (*KeyFound, error) {
	for name, jwks := range multiJWKS.sets {
		key, err := jwks.Keyfunc(token)
		if err == nil {
			return &KeyFound{
				Key:  key,
				Name: name.Name,
			}, nil
		}
	}
	return nil, fmt.Errorf("failed to find key ID in multiple JWKS: %w", ErrKIDNotFound)
}

type KeyFound struct {
	Key  interface{}
	Name string
}

func (k *KeyFuncMulti) Keyfunc(token *jwt.Token) (interface{}, error) {
	return k.multiJWKS.Keyfunc(token)
}

// MultiJWTKeyFunc returns a jwt.Keyfunc with multiple keyfunc.
//
// Doesn't support introspect and noops, it will ignore them.
func MultiJWTKeyFunc(providers []InfProviderCert, opts ...OptionJWK) (*JwkKeyFuncParse, error) {
	opt := GetOptionJWK(opts...)
	keyFuncOpt := MapOptionKeyfunc(opt)

	multi := map[MultipleJWKSKey]keyfunc.Options{}
	for _, provider := range providers {
		certURL := provider.GetCertURL()
		if certURL == "" {
			return nil, fmt.Errorf("no cert URL")
		}

		multi[MultipleJWKSKey{
			URL:  certURL,
			Name: provider.GetName(),
		}] = keyFuncOpt
	}

	if len(multi) == 0 && opt.KeyFunc != nil {
		return &JwkKeyFuncParse{
			KeyFunc: opt.KeyFunc.Keyfunc,
		}, nil
	}

	multiKeyFunc := &KeyFuncMulti{
		givenJwks: opt.KeyFunc,
	}
	jwks, err := GetMultiple(multi, MultipleOptions{
		KeySelector: multiKeyFunc.KeySelectorFirst,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to getMultiple: %w", err)
	}

	multiKeyFunc.multiJWKS = jwks

	return &JwkKeyFuncParse{
		KeyFunc: multiKeyFunc.Keyfunc,
	}, nil
}

func GetOptionJWK(opts ...OptionJWK) optionsJWK {
	option := optionsJWK{
		RefreshErrorHandler: func(err error) {
			slog.Warn("failed to refresh jwt.Keyfunc", "err", err.Error())
		},
		RefreshInterval: time.Minute * 5,
		Ctx:             context.Background(),
	}

	for _, opt := range opts {
		opt(&option)
	}

	return option
}

func MapOptionKeyfunc(opt optionsJWK) keyfunc.Options {
	return keyfunc.Options{
		Ctx:                 opt.Ctx,
		RefreshErrorHandler: opt.RefreshErrorHandler,
		// RefreshRateLimit:    time.Minute * 5,
		RefreshInterval: opt.RefreshInterval,
		Client:          opt.Client,
	}
}

type optionsJWK struct {
	Client              *http.Client
	RefreshErrorHandler func(err error)
	RefreshInterval     time.Duration
	Ctx                 context.Context
	Introspect          bool
	KeyFunc             InfKeyFunc
}

type OptionJWK func(options *optionsJWK)

// WithGivenKeys is used to set the given keys used to verify the token.
//
// Return ErrKIDNotFound if the kid is not found.
//
// Example:
//
//	// Create the JWKS from the given keys.
//	givenKeys := map[string]keyfunc.GivenKey{
//		"my-key-id": keyfunc.NewGivenHMAC(...),
//	}
//	jwks := keyfunc.NewGiven(givenKeys)
func WithKeyFunc(keyFunc InfKeyFunc) OptionJWK {
	return func(options *optionsJWK) {
		options.KeyFunc = keyFunc
	}
}

func WithIntrospect(v bool) OptionJWK {
	return func(options *optionsJWK) {
		options.Introspect = v
	}
}

// WithRefreshErrorHandler sets the refresh error handler for the jwt.Key.
func WithRefreshErrorHandler(fn func(err error)) OptionJWK {
	return func(options *optionsJWK) {
		options.RefreshErrorHandler = fn
	}
}

// WithRefreshInterval sets the refresh interval for the jwt.Keyfunc default is 5 minutes.
func WithRefreshInterval(d time.Duration) OptionJWK {
	return func(options *optionsJWK) {
		options.RefreshInterval = d
	}
}

// WithClient is used to set the http.Client used to fetch the JWKs.
func WithClient(client *http.Client) OptionJWK {
	return func(options *optionsJWK) {
		options.Client = client
	}
}

// WithContext is used to set the context used to fetch the JWKs.
func WithContext(ctx context.Context) OptionJWK {
	return func(options *optionsJWK) {
		options.Ctx = ctx
	}
}

// GetMultiple creates a new MultipleJWKS. A map of length one or more JWKS URLs to Options is required.
//
// Be careful when choosing Options for each JWKS in the map. If RefreshUnknownKID is set to true for all JWKS in the
// map then many refresh requests would take place each time a JWT is processed, this should be rate limited by
// RefreshRateLimit.
func GetMultiple(multiple map[MultipleJWKSKey]keyfunc.Options, options MultipleOptions) (multiJWKS *MultipleJWKS, err error) {
	if len(multiple) < 1 {
		return nil, fmt.Errorf("multiple JWKS must have one or more remote JWK Set resources: %w", keyfunc.ErrMultipleJWKSSize)
	}

	multiJWKS = &MultipleJWKS{
		sets:        make(map[MultipleJWKSKey]*keyfunc.JWKS, len(multiple)),
		keySelector: options.KeySelector,
	}

	for u, opts := range multiple {
		jwks, err := keyfunc.Get(u.URL, opts)
		if err != nil {
			return nil, fmt.Errorf("failed to get JWKS from %q: %w", u, err)
		}
		multiJWKS.sets[MultipleJWKSKey{
			URL:  u.URL,
			Name: u.Name,
		}] = jwks
	}

	return multiJWKS, nil
}

// MultipleJWKS manages multiple JWKS and has a field for jwt.Keyfunc.
type MultipleJWKS struct {
	keySelector func(multiJWKS *MultipleJWKS, token *jwt.Token) (key interface{}, err error)
	sets        map[MultipleJWKSKey]*keyfunc.JWKS // No lock is required because this map is read-only after initialization.
}

// Keyfunc matches the signature of github.com/golang-jwt/jwt/v5's jwt.Keyfunc function.
func (m *MultipleJWKS) Keyfunc(token *jwt.Token) (interface{}, error) {
	return m.keySelector(m, token)
}

type MultipleJWKSKey struct {
	URL  string
	Name string
}

type MultipleOptions struct {
	KeySelector func(multiJWKS *MultipleJWKS, token *jwt.Token) (key interface{}, err error)
}
