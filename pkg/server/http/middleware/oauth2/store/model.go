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
	// ClientID of the original authorization request; carried into the
	// issued code so the token endpoint can bind the redemption.
	ClientID string `json:"client_id,omitempty"`
	// Resources are RFC 8707 resource indicators from the authorization request.
	Resources []string `json:"resources,omitempty"`
	// BrowserBindingHash binds the provider callback to the browser that
	// initiated the authorization request.
	BrowserBindingHash string `json:"browser_binding_hash,omitempty"`
}

type Code struct {
	Alias string   `json:"alias"`
	Scope []string `json:"scope"`
	Nonce string   `json:"nonce"`
	// PKCE (RFC 7636) challenge to verify at the token endpoint.
	CodeChallenge       string `json:"code_challenge,omitempty"`
	CodeChallengeMethod string `json:"code_challenge_method,omitempty"`
	// ClientID binds the code to the requesting client. The auth middleware
	// rejects codes where this binding is missing or belongs to another client.
	ClientID string `json:"client_id,omitempty"`
	// RedirectURI binds the code to the redirect target of the authorization
	// request. The auth middleware requires the same redirect_uri at exchange.
	RedirectURI string `json:"redirect_uri,omitempty"`
	// Resources are RFC 8707 resource indicators requested during
	// authorization; they end up in the access token audience.
	Resources []string `json:"resources,omitempty"`
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
