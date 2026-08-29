package login

import (
	"io"
	"net/http"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/session"
)

type enrollmentStatusResponse struct {
	Payload session.PasskeyEnrollmentStatus `json:"payload"`
}

func (m *Login) enrollmentSession(w http.ResponseWriter, r *http.Request) (session.InfPasskeyEnrollment, string, string, bool) {
	claims, logged, err := m.session.IsLogged(w, r)
	if err != nil || !logged {
		writeError(w, http.StatusUnauthorized, "authenticated session required")

		return nil, "", "", false
	}

	_, providerName, err := m.session.GetTokenData(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authenticated session required")

		return nil, "", "", false
	}
	provider, ok := m.session.GetProvider(providerName)
	if !ok || provider.AuthMiddleware == "" {
		return nil, "", "", true
	}

	issuer, ok := session.IssuerRegistry.Get(provider.AuthMiddleware).(session.InfPasskeyEnrollment)
	if !ok {
		return nil, "", "", true
	}

	userID, _ := claims.Map["sub"].(string)
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "session subject is missing")

		return nil, "", "", false
	}

	method, err := m.session.GetAuthenticationMethod(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "login method is missing")

		return nil, "", "", false
	}

	return issuer, userID, method, true
}

func (m *Login) PasskeyEnrollmentStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")

	issuer, userID, method, ok := m.enrollmentSession(w, r)
	if !ok {
		return
	}
	if issuer == nil {
		httputil.JSON(w, http.StatusOK, enrollmentStatusResponse{})

		return
	}

	status, err := issuer.PasskeyEnrollmentStatus(r.Context(), userID, method)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot check passkey enrollment")

		return
	}

	httputil.JSON(w, http.StatusOK, enrollmentStatusResponse{Payload: status})
}

func (m *Login) PasskeyEnrollmentRegister(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")

	issuer, userID, method, ok := m.enrollmentSession(w, r)
	if !ok {
		return
	}
	if issuer == nil {
		writeError(w, http.StatusForbidden, "passkey enrollment is not available")

		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "cannot read enrollment request")

		return
	}

	respBody, statusCode, err := issuer.PasskeyEnrollmentRegister(r.Context(), r, userID, method, body)
	if err != nil {
		writeError(w, statusCode, err.Error())

		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_, _ = w.Write(respBody)
}
