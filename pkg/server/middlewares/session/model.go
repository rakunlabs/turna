package session

import (
	"encoding/base64"
	"encoding/json"
)

type MetaData struct {
	Error string `json:"error"`
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
