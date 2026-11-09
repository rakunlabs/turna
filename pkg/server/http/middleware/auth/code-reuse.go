package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"time"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/auth/data"
)

// codeUseTTL keeps a redeemed authorization code's session id long enough
// to catch a replay (codes themselves expire much sooner).
const codeUseTTL = 30 * time.Minute

// codeUse records the session family created from a redeemed code.
type codeUse struct {
	SID      string `json:"sid"`
	Subject  string `json:"sub,omitempty"`
	ClientID string `json:"azp,omitempty"`
}

func codeUseID(code string) string {
	sum := sha256.Sum256([]byte(code))

	return hex.EncodeToString(sum[:])
}

// rememberCodeUse records which session a redeemed code created.
func (m *Auth) rememberCodeUse(ctx context.Context, code string, use codeUse) {
	if m.store == nil {
		return
	}

	if err := m.store.PutFlowCode(ctx, flowKindCodeUsed, codeUseID(code), use, codeUseTTL); err != nil {
		slog.Warn("auth: cannot record authorization code use", "error", err.Error())
	}
}

// revokeReusedCode revokes the session issued from code when it was already
// redeemed (RFC 6749 §4.1.2, RFC 9700 §4.5): a second redemption means the
// code leaked, so its tokens must stop working. It reports whether code had
// been used.
func (m *Auth) revokeReusedCode(ctx context.Context, code string) bool {
	if m.store == nil {
		return false
	}

	var use codeUse
	if err := m.store.GetFlowCode(ctx, flowKindCodeUsed, codeUseID(code), &use); err != nil {
		if !errors.Is(err, data.ErrNotFound) {
			slog.Warn("auth: cannot read authorization code use", "error", err.Error())
		}

		return false
	}

	if use.SID == "" {
		return true
	}

	payload := revokedToken{Subject: use.Subject, ClientID: use.ClientID, Type: "Code"}
	if _, err := m.store.CreateFlowCodeOnce(ctx, flowKindRevokedSession, use.SID, payload, m.cache.Snapshot().Token.GetRefreshAbsoluteLifetime()); err != nil {
		slog.Warn("auth: cannot revoke session of a reused code", "error", err.Error())
	}

	slog.Warn("authorization code reuse detected, session revoked", "sub", use.Subject, "azp", use.ClientID)
	m.audit(nil, AuditEvent{Event: auditCodeReuse, Outcome: auditFailure, Subject: use.Subject, ClientID: use.ClientID, Detail: map[string]any{"sid": use.SID}})

	return true
}
