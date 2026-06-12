package store

import (
	"encoding/base64"
	"encoding/json"
)

// /////////////////////////

type State struct {
	RedirectURI string   `json:"redirect_uri"`
	State       string   `json:"state"`
	OrgState    string   `json:"org_state"`
	Scope       []string `json:"scope"`
	Nonce       string   `json:"nonce"`
	// PKCE (RFC 7636) challenge carried from the authorization request.
	CodeChallenge       string `json:"code_challenge,omitempty"`
	CodeChallengeMethod string `json:"code_challenge_method,omitempty"`
}

type Code struct {
	Alias string   `json:"alias"`
	Scope []string `json:"scope"`
	Nonce string   `json:"nonce"`
	// PKCE (RFC 7636) challenge to verify at the token endpoint.
	CodeChallenge       string `json:"code_challenge,omitempty"`
	CodeChallengeMethod string `json:"code_challenge_method,omitempty"`
}

func Encode[T any](state T) (string, error) {
	v, err := json.Marshal(state)
	v64 := base64.StdEncoding.EncodeToString(v)

	return v64, err
}

func Decode[T any](encoded string) (T, error) {
	var ret T

	v, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return ret, err
	}

	err = json.Unmarshal(v, &ret)

	return ret, err
}
