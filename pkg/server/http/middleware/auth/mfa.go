package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/auth/data"
)

var errTOTPLocked = errors.New("too many invalid second-factor attempts; try again in five minutes")

// ConsumeTOTP serializes verification across replicas. Both TOTP and recovery
// attempts share a five-attempt budget and a five-minute cooldown. A successful
// authenticator counter cannot be used again, including across login grants.
// confirm is only used by the authenticated enrollment endpoint.
func (s *Store) ConsumeTOTP(ctx context.Context, userID, code string, skew int, confirm bool) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback() //nolint:errcheck
	var encrypted string
	var confirmed bool
	var last int64
	var attempts int
	var locked sql.NullTime
	var recovery []byte
	err = tx.QueryRowContext(ctx, `SELECT secret_encrypted, confirmed, last_counter,
		failed_attempts, locked_until, recovery_codes FROM auth_totp_secrets
		WHERE user_id = $1 FOR UPDATE`, userID).Scan(&encrypted, &confirmed, &last, &attempts, &locked, &recovery)
	if err != nil {
		return false, err
	}
	now := time.Now()
	if locked.Valid && now.Before(locked.Time) {
		return false, errTOTPLocked
	}
	if locked.Valid {
		attempts = 0
	}
	if confirm && confirmed {
		return false, data.ErrConflict
	}
	if !confirm && !confirmed {
		return false, data.ErrConflict
	}
	secret, err := s.cipher.DecryptString(encrypted)
	if err != nil {
		return false, err
	}
	counter, valid := matchingTOTPCounter(secret, code, skew, now)
	valid = valid && counter > last
	var codes []string
	if err := json.Unmarshal(recovery, &codes); err != nil {
		return false, err
	}
	if !valid && !confirm {
		hash := hashAPIKey(code)
		for i, stored := range codes {
			if stored == hash {
				codes = append(codes[:i], codes[i+1:]...)
				valid = true
				counter = last
				break
			}
		}
	}
	if valid {
		raw, err := json.Marshal(codes)
		if err != nil {
			return false, err
		}
		_, err = tx.ExecContext(ctx, `UPDATE auth_totp_secrets SET last_counter = $2,
			confirmed = true, failed_attempts = 0, locked_until = NULL,
			recovery_codes = $3::jsonb, updated_at = now() WHERE user_id = $1`, userID, counter, raw)
		if err != nil {
			return false, err
		}
	} else {
		attempts++
		var until any
		if attempts >= 5 {
			until = now.Add(5 * time.Minute)
		}
		_, err = tx.ExecContext(ctx, `UPDATE auth_totp_secrets SET failed_attempts = $2,
			locked_until = $3 WHERE user_id = $1`, userID, attempts, until)
		if err != nil {
			return false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	if !valid && attempts >= 5 {
		return false, errTOTPLocked
	}
	return valid, nil
}

// requireLoginTOTP is shared by password and email-code grants. Missing codes
// do not spend attempts, allowing a UI to discover the MFA challenge and retry.
func (m *Auth) requireLoginTOTP(w http.ResponseWriter, r *http.Request, userID, code string) bool {
	cfg := m.cache.Snapshot().TOTP
	if cfg.Disabled {
		return true
	}
	_, confirmed, err := m.store.GetTOTPSecret(r.Context(), userID)
	if errors.Is(err, data.ErrNotFound) {
		return true
	}
	if err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{Error: "server_error", ErrorDescription: "cannot check totp", code: http.StatusInternalServerError})
		return false
	}
	if !confirmed {
		return true
	}
	if code == "" {
		httputil.HandleError(w, AccessTokenErrorResponse{Error: "mfa_required", ErrorDescription: "totp code required", code: http.StatusUnauthorized})
		return false
	}
	ok, err := m.store.ConsumeTOTP(r.Context(), userID, code, cfg.GetSkew(), false)
	switch {
	case errors.Is(err, errTOTPLocked):
		w.Header().Set("Retry-After", "300")
		httputil.HandleError(w, AccessTokenErrorResponse{Error: "invalid_grant", ErrorDescription: err.Error(), code: http.StatusTooManyRequests})
	case err != nil:
		httputil.HandleError(w, AccessTokenErrorResponse{Error: "server_error", ErrorDescription: "cannot verify totp", code: http.StatusInternalServerError})
	case !ok:
		httputil.HandleError(w, AccessTokenErrorResponse{Error: "invalid_grant", ErrorDescription: "totp code invalid or already used", code: http.StatusUnauthorized})
	default:
		return true
	}
	return false
}
