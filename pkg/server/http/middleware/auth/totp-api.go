package auth

import (
	"errors"
	"net/http"
	"time"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data"
)

type TOTPRegisterResponse struct {
	Secret string `json:"secret"`
	URL    string `json:"url"`
}

type TOTPConfirmRequest struct {
	Code string `json:"code"`
}

// TOTPStatusAPI reports whether the X-User has a confirmed totp secret.
func (m *Auth) TOTPStatusAPI(w http.ResponseWriter, r *http.Request) {
	user := m.xUser(w, r)
	if user == nil {
		return
	}

	enabled := false
	if _, confirmed, err := m.store.GetTOTPSecret(r.Context(), user.ID); err == nil {
		enabled = confirmed
	}

	httputil.JSON(w, http.StatusOK, Response[map[string]any]{
		Payload: map[string]any{"enabled": enabled},
	})
}

// TOTPRegisterAPI generates a fresh (unconfirmed) totp secret for the X-User.
func (m *Auth) TOTPRegisterAPI(w http.ResponseWriter, r *http.Request) {
	cfg := m.cache.Snapshot().TOTP
	if cfg.Disabled {
		httputil.HandleError(w, httputil.NewError("totp is disabled", nil, http.StatusForbidden))
		return
	}

	user := m.xUser(w, r)
	if user == nil {
		return
	}

	secret, err := generateTOTPSecret()
	if err != nil {
		httputil.HandleError(w, httputil.NewError("cannot generate totp secret", err, http.StatusInternalServerError))
		return
	}

	if err := m.store.UpsertTOTPSecret(r.Context(), user.ID, secret); err != nil {
		httputil.HandleError(w, httputil.NewError("cannot save totp secret", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, Response[TOTPRegisterResponse]{
		Payload: TOTPRegisterResponse{
			Secret: secret,
			URL:    totpProvisioningURL(cfg.GetIssuer(), r.Header.Get("X-User"), secret),
		},
	})
}

// TOTPConfirmAPI verifies a code and activates totp for the X-User.
func (m *Auth) TOTPConfirmAPI(w http.ResponseWriter, r *http.Request) {
	cfg := m.cache.Snapshot().TOTP

	user := m.xUser(w, r)
	if user == nil {
		return
	}

	var req TOTPConfirmRequest
	if err := httputil.Decode(r, &req); err != nil {
		httputil.HandleError(w, httputil.NewError("cannot decode request", err, http.StatusBadRequest))
		return
	}

	secret, _, err := m.store.GetTOTPSecret(r.Context(), user.ID)
	if err != nil {
		httputil.HandleError(w, httputil.NewError("totp not registered", err, http.StatusNotFound))
		return
	}

	if !validateTOTP(secret, req.Code, cfg.GetSkew(), time.Now()) {
		httputil.HandleError(w, httputil.NewError("totp code invalid", nil, http.StatusBadRequest))
		return
	}

	if err := m.store.ConfirmTOTPSecret(r.Context(), user.ID); err != nil {
		httputil.HandleError(w, httputil.NewError("cannot confirm totp", err, http.StatusInternalServerError))
		return
	}

	// recovery codes are shown exactly once; only hashes are stored
	codes, hashes, err := generateTOTPRecoveryCodes()
	if err != nil {
		httputil.HandleError(w, httputil.NewError("cannot generate recovery codes", err, http.StatusInternalServerError))
		return
	}

	if err := m.store.SetTOTPRecoveryCodes(r.Context(), user.ID, hashes); err != nil {
		httputil.HandleError(w, httputil.NewError("cannot save recovery codes", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, Response[map[string]any]{
		Payload: map[string]any{
			"message":        "totp enabled",
			"recovery_codes": codes,
		},
	})
}

// TOTPRecoveryAPI regenerates recovery codes; the old set becomes invalid.
func (m *Auth) TOTPRecoveryAPI(w http.ResponseWriter, r *http.Request) {
	user := m.xUser(w, r)
	if user == nil {
		return
	}

	if _, confirmed, err := m.store.GetTOTPSecret(r.Context(), user.ID); err != nil || !confirmed {
		httputil.HandleError(w, httputil.NewError("totp is not enabled", nil, http.StatusBadRequest))
		return
	}

	codes, hashes, err := generateTOTPRecoveryCodes()
	if err != nil {
		httputil.HandleError(w, httputil.NewError("cannot generate recovery codes", err, http.StatusInternalServerError))
		return
	}

	if err := m.store.SetTOTPRecoveryCodes(r.Context(), user.ID, hashes); err != nil {
		httputil.HandleError(w, httputil.NewError("cannot save recovery codes", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, Response[map[string]any]{
		Payload: map[string]any{
			"message":        "recovery codes regenerated",
			"recovery_codes": codes,
		},
	})
}

// TOTPDeleteAPI removes the X-User's totp secret.
func (m *Auth) TOTPDeleteAPI(w http.ResponseWriter, r *http.Request) {
	user := m.xUser(w, r)
	if user == nil {
		return
	}

	if err := m.store.DeleteTOTPSecret(r.Context(), user.ID); err != nil {
		code := http.StatusInternalServerError
		if errors.Is(err, data.ErrNotFound) {
			code = http.StatusNotFound
		}

		httputil.HandleError(w, httputil.NewError("cannot delete totp", err, code))

		return
	}

	httputil.JSON(w, http.StatusOK, Response[map[string]any]{
		Payload: map[string]any{"message": "totp disabled"},
	})
}
