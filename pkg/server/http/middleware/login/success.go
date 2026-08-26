package login

import (
	"net/http"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/oauth2/auth"
)

const (
	// flowQueryParam carries the SDK minted popup correlation id into the
	// code flow. Keep it in sync with the SDK.
	flowQueryParam = "turna_flow"
	// flowIDMaxLength caps the SDK supplied correlation id; it only has to
	// be unique per open popup, not unguessable.
	flowIDMaxLength = 64
)

// sanitizeFlowID keeps the id usable as a cookie-name suffix. Anything
// unexpected drops the whole id so the flow falls back to the shared
// success cookie instead of minting a cookie with an attacker chosen name.
func sanitizeFlowID(flow string) string {
	if flow == "" || len(flow) > flowIDMaxLength {
		return ""
	}

	for _, r := range flow {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '-', r == '_':
		default:
			return ""
		}
	}

	return flow
}

// successCookie scopes the completion marker to a single popup flow.
//
// The shared name lets any finished sign-in on the origin satisfy every
// waiting opener: with nested login windows the innermost completion made
// the outermost login page resolve early, close the intermediate window
// mid-redirect and navigate without ever getting a session.
func (m *Login) successCookie(flow string) *auth.Cookie {
	flow = sanitizeFlowID(flow)
	if flow == "" {
		return &m.SuccessCookie
	}

	cookie := m.SuccessCookie
	cookie.CookieName = m.SuccessCookie.CookieName + "_" + flow

	return &cookie
}

func (m *Login) SetSuccess(w http.ResponseWriter, flow, success string) {
	auth.SetCookie(w, success, m.successCookie(flow))
}

func (m *Login) GetSuccess(r http.Request, flow string) (string, error) {
	cookie, err := r.Cookie(m.successCookie(flow).CookieName)
	if err != nil {
		return "", err
	}

	return cookie.Value, nil
}

func (m *Login) RemoveSuccess(w http.ResponseWriter, flow string) {
	auth.RemoveCookie(w, m.successCookie(flow))
}
