package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // RFC 6238 default algorithm
	"crypto/subtle"
	"database/sql"
	"encoding/base32"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data"
)

const (
	totpPeriod = 30
	totpDigits = 6
)

var totpBase32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// generateTOTPSecret returns a new base32 encoded shared secret.
func generateTOTPSecret() (string, error) {
	raw := make([]byte, 20)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}

	return totpBase32.EncodeToString(raw), nil
}

// totpCode computes the RFC 6238 code for the given counter step.
func totpCode(secret string, counter uint64) (string, error) {
	key, err := totpBase32.DecodeString(secret)
	if err != nil {
		return "", fmt.Errorf("invalid totp secret: %w", err)
	}

	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], counter)

	mac := hmac.New(sha1.New, key)
	mac.Write(buf[:])
	sum := mac.Sum(nil)

	offset := sum[len(sum)-1] & 0x0f
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff

	code := value % 1000000 // 10^totpDigits

	return fmt.Sprintf("%0*d", totpDigits, code), nil
}

// validateTOTP checks the code against the secret allowing +/- skew periods.
func validateTOTP(secret, code string, skew int, now time.Time) bool {
	if len(code) != totpDigits {
		return false
	}
	if _, err := strconv.Atoi(code); err != nil {
		return false
	}

	counter := uint64(now.Unix() / totpPeriod) //nolint:gosec // unix time is positive

	for i := -skew; i <= skew; i++ {
		c := int64(counter) + int64(i)
		if c < 0 {
			continue
		}

		expected, err := totpCode(secret, uint64(c))
		if err != nil {
			return false
		}

		if subtle.ConstantTimeCompare([]byte(expected), []byte(code)) == 1 {
			return true
		}
	}

	return false
}

// totpProvisioningURL builds the otpauth:// URL for authenticator apps.
func totpProvisioningURL(issuer, account, secret string) string {
	label := url.PathEscape(issuer + ":" + account)

	q := url.Values{}
	q.Set("secret", secret)
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", strconv.Itoa(totpDigits))
	q.Set("period", strconv.Itoa(totpPeriod))

	return "otpauth://totp/" + label + "?" + q.Encode()
}

// ////////////////////////////////////////////////////////////////////
// store

// UpsertTOTPSecret stores a fresh (unconfirmed) totp secret for the user.
func (s *Store) UpsertTOTPSecret(ctx context.Context, userID, secret string) error {
	encrypted, err := s.cipher.EncryptString(secret)
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx, `INSERT INTO auth_totp_secrets (user_id, secret_encrypted, confirmed)
		VALUES ($1, $2, false)
		ON CONFLICT (user_id) DO UPDATE SET
			secret_encrypted = EXCLUDED.secret_encrypted,
			confirmed = false,
			updated_at = now()`, userID, encrypted)

	return err
}

// GetTOTPSecret loads the user's totp secret and confirmation state.
func (s *Store) GetTOTPSecret(ctx context.Context, userID string) (string, bool, error) {
	var encrypted string
	var confirmed bool

	err := s.db.QueryRowContext(ctx,
		`SELECT secret_encrypted, confirmed FROM auth_totp_secrets WHERE user_id = $1`, userID,
	).Scan(&encrypted, &confirmed)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, fmt.Errorf("totp not registered; %w", data.ErrNotFound)
		}

		return "", false, err
	}

	secret, err := s.cipher.DecryptString(encrypted)
	if err != nil {
		return "", false, err
	}

	return secret, confirmed, nil
}

// ConfirmTOTPSecret marks the user's totp secret as confirmed.
func (s *Store) ConfirmTOTPSecret(ctx context.Context, userID string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE auth_totp_secrets SET confirmed = true, updated_at = now() WHERE user_id = $1`, userID)
	if err != nil {
		return err
	}

	if count, err := res.RowsAffected(); err == nil && count == 0 {
		return fmt.Errorf("totp not registered; %w", data.ErrNotFound)
	}

	return nil
}

// totpRecoveryCodeCount is how many recovery codes a set contains.
const totpRecoveryCodeCount = 8

// generateTOTPRecoveryCodes returns plaintext codes and their sha256 hashes.
func generateTOTPRecoveryCodes() ([]string, []string, error) {
	codes := make([]string, 0, totpRecoveryCodeCount)
	hashes := make([]string, 0, totpRecoveryCodeCount)

	for range totpRecoveryCodeCount {
		raw := make([]byte, 10)
		if _, err := rand.Read(raw); err != nil {
			return nil, nil, err
		}

		encoded := strings.ToLower(totpBase32.EncodeToString(raw))
		code := encoded[:8] + "-" + encoded[8:16]

		codes = append(codes, code)
		hashes = append(hashes, hashAPIKey(code))
	}

	return codes, hashes, nil
}

// SetTOTPRecoveryCodes replaces the user's recovery code hashes.
func (s *Store) SetTOTPRecoveryCodes(ctx context.Context, userID string, hashes []string) error {
	raw, err := json.Marshal(hashes)
	if err != nil {
		return err
	}

	res, err := s.db.ExecContext(ctx,
		`UPDATE auth_totp_secrets SET recovery_codes = $2::jsonb, updated_at = now() WHERE user_id = $1`,
		userID, string(raw))
	if err != nil {
		return err
	}

	if count, err := res.RowsAffected(); err == nil && count == 0 {
		return fmt.Errorf("totp not registered; %w", data.ErrNotFound)
	}

	return nil
}

// ConsumeTOTPRecoveryCode validates and removes a recovery code; each code
// is single use.
func (s *Store) ConsumeTOTPRecoveryCode(ctx context.Context, userID, code string) bool {
	hash := hashAPIKey(code)

	res, err := s.db.ExecContext(ctx, `UPDATE auth_totp_secrets
		SET recovery_codes = recovery_codes - $2, updated_at = now()
		WHERE user_id = $1 AND recovery_codes ? $2`,
		userID, hash)
	if err != nil {
		return false
	}

	count, err := res.RowsAffected()

	return err == nil && count > 0
}

// DeleteTOTPSecret removes the user's totp secret.
func (s *Store) DeleteTOTPSecret(ctx context.Context, userID string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM auth_totp_secrets WHERE user_id = $1`, userID)
	if err != nil {
		return err
	}

	if count, err := res.RowsAffected(); err == nil && count == 0 {
		return fmt.Errorf("totp not registered; %w", data.ErrNotFound)
	}

	return nil
}
