package token

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var defaultParser = jwt.NewParser()

type JWT struct {
	private interface{}
	public  interface{}
	method  jwt.SigningMethod
	kid     string
}

// NewJWT function get private key and options and return a new JWT instance.
//
// Default expiration function is time.Now().Add(time.Hour).Unix().
func NewJWT(opts ...OptionJWT) (*JWT, error) {
	o := optionJWT{}

	for _, opt := range opts {
		opt(&o)
	}

	if o.kid == "" {
		return nil, errors.New("kid is required")
	}

	if o.method == nil {
		return nil, errors.New("method is required")
	}

	var private interface{}
	var public interface{}
	switch o.method.(type) {
	case *jwt.SigningMethodHMAC:
		private = o.secretHMAC
		public = o.secretHMAC
	case *jwt.SigningMethodRSAPSS:
		private = o.secretRSAPrivate
		if o.secretRSAPublic != nil {
			public = o.secretRSAPublic
		} else {
			public = o.secretRSAPrivate.Public()
		}
	case *jwt.SigningMethodRSA:
		private = o.secretRSAPrivate
		if o.secretRSAPublic != nil {
			public = o.secretRSAPublic
		} else {
			public = o.secretRSAPrivate.Public()
		}
	case *jwt.SigningMethodECDSA:
		private = o.secretECDSAPrivate
		if o.secretECDSAPublic != nil {
			public = o.secretECDSAPublic
		} else {
			public = o.secretECDSAPrivate.Public()
		}
	case *jwt.SigningMethodEd25519:
		private = o.secretED25519Private
		if o.secretED25519Public != nil {
			public = o.secretED25519Public
		} else {
			public = o.secretED25519Private.Public()
		}
	default:
		return nil, errors.New("unsupported method")
	}

	if private == nil || public == nil {
		return nil, errors.New("private and public key is required")
	}

	return &JWT{
		private: private,
		public:  public,
		method:  o.method,
		kid:     o.kid,
	}, nil
}

func (t *JWT) Alg() string {
	return t.method.Alg()
}

// Generate function get custom values and add 'exp' as expires at with expDate argument with unix format.
func (t *JWT) Generate(mapClaims map[string]interface{}, expDate int64) (string, error) {
	if expDate > 0 {
		mapClaims["exp"] = expDate
	}

	if _, ok := mapClaims["typ"]; !ok {
		mapClaims["typ"] = "Bearer"
	}
	mapClaims["iat"] = time.Now().Unix()

	token := jwt.NewWithClaims(t.method, jwt.MapClaims(mapClaims))

	// header part
	if t.kid != "" {
		token.Header["kid"] = t.kid
	}
	if t.method.Alg() != "" {
		token.Header["alg"] = t.method.Alg()
	}

	tokenString, err := token.SignedString(t.private)
	if err != nil {
		err = fmt.Errorf("cannot sign: %w", err)
	}

	return tokenString, err
}

// Parse is validating and getting claims.
func (t *JWT) Parse(tokenStr string, claims jwt.Claims) (*jwt.Token, error) {
	token, err := jwt.ParseWithClaims(
		tokenStr,
		claims,
		func(token *jwt.Token) (interface{}, error) {
			return t.public, nil
		},
		jwt.WithValidMethods([]string{t.method.Alg()}),
	)
	if err != nil {
		return nil, fmt.Errorf("token validate: %w", err)
	}

	return token, nil
}

// Renew token with not changing claims.
func (t *JWT) Renew(tokenStr string, expDate int64) (string, error) {
	claims := jwt.MapClaims{}
	if _, err := t.Parse(tokenStr, &claims); err != nil {
		return "", fmt.Errorf("renew: %w", err)
	}

	return t.Generate(claims, expDate)
}

func ParseUnverified(tokenString string, claims jwt.Claims) (*jwt.Token, []string, error) {
	return defaultParser.ParseUnverified(tokenString, claims)
}

func ParseAccessToken(tokenValue []byte, claims jwt.Claims) (*jwt.Token, []string, error) {
	v := accessToken{}
	if err := json.Unmarshal(tokenValue, &v); err != nil {
		return nil, nil, err
	}

	return ParseUnverified(v.AccessToken, claims)
}
