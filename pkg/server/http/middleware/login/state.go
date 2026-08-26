package login

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/oauth2/auth"
)

// flowSeparator splits the optional popup flow id from the state value.
// The state is base64url so the byte never appears inside it.
const flowSeparator = "|"

// StateInfo is the per-flow data parked in the state cookie while the
// browser is away at the provider.
type StateInfo struct {
	RememberMe bool
	// Flow is the popup correlation id minted by the SDK. It scopes the
	// success cookie so only the window that started this exact flow is
	// notified when it completes.
	Flow string
}

// stateCookie derives a per-flow cookie from the configured base name.
//
// One shared name breaks nested and concurrent code flows: a login page
// opened inside a popup starts its own flow, overwrites the outer state
// and then deletes the cookie when its callback consumes it, so the outer
// callback later fails with "state is not valid".
func (m *Login) stateCookie(state string) *auth.Cookie {
	cookie := m.StateCookie
	sum := sha256.Sum256([]byte(state))
	cookie.CookieName = m.StateCookie.CookieName + "_" + hex.EncodeToString(sum[:])

	return &cookie
}

func encodeStateValue(state string, info StateInfo) string {
	if info.RememberMe {
		state += ".remember"
	}

	if info.Flow != "" {
		state += flowSeparator + info.Flow
	}

	return state
}

func decodeStateValue(value string) (string, StateInfo) {
	raw, flow, _ := strings.Cut(value, flowSeparator)
	state, rememberMe := strings.CutSuffix(raw, ".remember")

	return state, StateInfo{RememberMe: rememberMe, Flow: sanitizeFlowID(flow)}
}

func (m *Login) SetState(w http.ResponseWriter, state string, info StateInfo) {
	auth.SetCookie(w, encodeStateValue(state, info), m.stateCookie(state))
}

func (m *Login) GetState(r *http.Request, check string) (string, StateInfo, error) {
	cookie, err := r.Cookie(m.stateCookie(check).CookieName)
	if err != nil {
		return "", StateInfo{}, err
	}

	state, info := decodeStateValue(cookie.Value)

	return state, info, nil
}

func (m *Login) CheckState(w http.ResponseWriter, r *http.Request, check string) (StateInfo, error) {
	state, info, err := m.GetState(r, check)
	if err != nil {
		return StateInfo{}, err
	}

	m.RemoveState(w, check)

	if state != check {
		return StateInfo{}, fmt.Errorf("state is not valid")
	}

	return info, nil
}

func (m *Login) RemoveState(w http.ResponseWriter, state string) {
	auth.RemoveCookie(w, m.stateCookie(state))
}
