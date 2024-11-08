package session

import (
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// DefaultExpireDuration is the default duration to check if the access token is about to expire.
var DefaultExpireDuration = time.Second * 10

// IsRefreshNeed checks if the access token is about to expire.
func IsRefreshNeed(accessToken string) (bool, error) {
	claims := jwt.RegisteredClaims{}

	_, _, err := jwt.NewParser().ParseUnverified(accessToken, &claims)
	if err != nil {
		return false, err
	}

	v, err := claims.GetExpirationTime()
	if err != nil {
		return false, err
	}

	return v.Before(time.Now().Add(DefaultExpireDuration)), nil
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
