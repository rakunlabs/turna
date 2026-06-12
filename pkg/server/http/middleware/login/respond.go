package login

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
)

// writeError responds with the standard error envelope:
//
//	{"message": "<human readable>", "error": "<machine detail, optional>"}
//
// It is the canonical shape shared with the auth middleware (httputil.Error),
// so every login endpoint and the UI agree on a single format.
func writeError(w http.ResponseWriter, code int, message string) {
	httputil.HandleError(w, httputil.NewError(message, nil, code))
}

// oauthError is the RFC 6749 token error body returned by upstream providers.
type oauthError struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// respondUpstreamError normalizes an upstream token error into the standard
// envelope. Upstream OAuth2 error bodies
// ({"error":"invalid_grant","error_description":"password not match"}) are
// unwrapped so the human-readable description surfaces as the message and the
// machine code is preserved in the error field, instead of leaking a raw JSON
// string to the client.
func respondUpstreamError(w http.ResponseWriter, code int, err error) {
	if code < http.StatusBadRequest {
		code = http.StatusInternalServerError
	}

	message := err.Error()
	var detail error

	var oe oauthError
	if json.Unmarshal([]byte(message), &oe) == nil && (oe.Error != "" || oe.ErrorDescription != "") {
		if oe.ErrorDescription != "" {
			message = oe.ErrorDescription
		} else {
			message = oe.Error
		}

		if oe.Error != "" {
			detail = errors.New(oe.Error)
		}
	}

	httputil.HandleError(w, httputil.NewError(message, detail, code))
}
