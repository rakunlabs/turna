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
}

type Code struct {
	Alias string   `json:"alias"`
	Scope []string `json:"scope"`
	Nonce string   `json:"nonce"`
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
