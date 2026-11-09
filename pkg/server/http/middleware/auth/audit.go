package auth

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/xhit/go-str2duration/v2"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
)

// Audit events.
const (
	auditLogin         = "login"          // password, email code, passkey, federated sign-in
	auditMFA           = "mfa"            // second factor check
	auditTokenIssue    = "token.issue"    // token endpoint grants
	auditTokenRefresh  = "token.refresh"  // refresh token redemption
	auditTokenRevoke   = "token.revoke"   // RFC 7009 revocation
	auditLogout        = "logout"         // end session
	auditCodeReuse     = "code.reuse"     // authorization code replay
	auditRefreshReuse  = "refresh.reuse"  // rotated refresh token replay
	auditAdminChange   = "admin.change"   // management API write
	auditKeyRotate     = "key.rotate"     // signing or encryption key rotation
	auditConsentDenied = "consent.denied" // user denied an authorization
	auditClientAuth    = "client.auth"    // failed client authentication

	auditSuccess = "success"
	auditFailure = "failure"
)

// AuditSettings is the decoded "audit" setting namespace.
type AuditSettings struct {
	// Disabled stops recording audit events.
	Disabled bool `json:"disabled"`
	// Retention is how long events are kept, default 90d.
	Retention string `json:"retention"`
	// WebhookURL receives every recorded event as a JSON POST.
	WebhookURL string `json:"webhook_url"`
	// WebhookSecret signs webhook bodies: X-Turna-Signature is
	// "sha256=" + hex(HMAC-SHA256(secret, timestamp + "." + body)) with the
	// unix timestamp in X-Turna-Timestamp.
	WebhookSecret string `json:"webhook_secret"`
	// WebhookEvents limits the webhook to these events; empty sends all.
	WebhookEvents []string `json:"webhook_events"`

	retention time.Duration
}

func (a AuditSettings) GetRetention() time.Duration {
	if a.retention > 0 {
		return a.retention
	}

	return 90 * 24 * time.Hour
}

func parseAuditSettings(a *AuditSettings) error {
	if a.Retention != "" {
		d, err := str2duration.ParseDuration(a.Retention)
		if err != nil || d <= 0 {
			return fmt.Errorf("retention must be a positive duration")
		}
		a.retention = d
	}
	if a.WebhookURL != "" && !strings.HasPrefix(a.WebhookURL, "https://") && !strings.HasPrefix(a.WebhookURL, "http://") {
		return fmt.Errorf("webhook_url must be an http(s) URL")
	}

	return nil
}

// AuditEvent is one audit log record.
type AuditEvent struct {
	ID        int64          `json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	Event     string         `json:"event"`
	Outcome   string         `json:"outcome"`
	Actor     string         `json:"actor,omitempty"`
	Subject   string         `json:"subject,omitempty"`
	ClientID  string         `json:"client_id,omitempty"`
	IP        string         `json:"ip,omitempty"`
	UserAgent string         `json:"user_agent,omitempty"`
	Detail    map[string]any `json:"detail,omitempty"`
}

// auditQueue buffers events for the background writer so request latency
// never depends on the audit store or the webhook.
const auditQueueSize = 4096

func requestIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}

	return host
}

// audit records an event. It never blocks; when the queue is full the event
// is logged and dropped.
func (m *Auth) audit(r *http.Request, ev AuditEvent) {
	if m.auditCh == nil || m.cache == nil || m.cache.Snapshot().Audit.Disabled {
		return
	}

	ev.CreatedAt = time.Now()
	if r != nil {
		ev.IP = requestIP(r)
		ev.UserAgent = r.UserAgent()
		if ev.Actor == "" {
			ev.Actor = requestPrincipal(r)
		}
	}
	if len(ev.UserAgent) > 512 {
		ev.UserAgent = ev.UserAgent[:512]
	}

	select {
	case m.auditCh <- ev:
	default:
		slog.Warn("auth: audit queue full, event dropped", "event", ev.Event, "outcome", ev.Outcome, "subject", ev.Subject)
	}
}

// runAudit writes queued events and delivers webhooks until ctx ends, then
// drains what is left.
func (m *Auth) runAudit(ctx context.Context) {
	client := &http.Client{Timeout: 5 * time.Second}

	write := func(ev AuditEvent) {
		writeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := m.store.InsertAuditEvent(writeCtx, &ev); err != nil {
			slog.Error("auth: cannot write audit event", "event", ev.Event, "error", err.Error())
		}

		cfg := m.cache.Snapshot().Audit
		if cfg.WebhookURL != "" && (len(cfg.WebhookEvents) == 0 || slices.Contains(cfg.WebhookEvents, ev.Event)) {
			if err := deliverAuditWebhook(writeCtx, client, cfg, ev); err != nil {
				slog.Warn("auth: audit webhook failed", "event", ev.Event, "error", err.Error())
			}
		}
	}

	for {
		select {
		case ev := <-m.auditCh:
			write(ev)
		case <-ctx.Done():
			for {
				select {
				case ev := <-m.auditCh:
					write(ev)
				default:
					return
				}
			}
		}
	}
}

func deliverAuditWebhook(ctx context.Context, client *http.Client, cfg AuditSettings, ev AuditEvent) error {
	body, err := json.Marshal(ev)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "turna-auth-audit")

	if cfg.WebhookSecret != "" {
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		mac := hmac.New(sha256.New, []byte(cfg.WebhookSecret))
		mac.Write([]byte(ts + "."))
		mac.Write(body)
		req.Header.Set("X-Turna-Timestamp", ts)
		req.Header.Set("X-Turna-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}

	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode < 200 || res.StatusCode > 299 {
		return fmt.Errorf("webhook answered %s", res.Status)
	}

	return nil
}

// InsertAuditEvent stores one event.
func (s *Store) InsertAuditEvent(ctx context.Context, ev *AuditEvent) error {
	var detail any
	if len(ev.Detail) > 0 {
		raw, err := json.Marshal(ev.Detail)
		if err != nil {
			return err
		}
		detail = string(raw)
	}

	return s.db.QueryRowContext(ctx,
		`INSERT INTO auth_audit_log (created_at, event, outcome, actor, subject, client_id, ip, user_agent, detail)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb) RETURNING id`,
		ev.CreatedAt, ev.Event, ev.Outcome, ev.Actor, ev.Subject, ev.ClientID, ev.IP, ev.UserAgent, detail,
	).Scan(&ev.ID)
}

// AuditFilter selects audit events; zero values match everything.
type AuditFilter struct {
	Event   string
	Outcome string
	Subject string
	Actor   string
	Since   time.Time
	Until   time.Time
	// Before pages backwards: only ids smaller than Before.
	Before int64
	Limit  int
}

// ListAuditEvents returns matching events, newest first.
func (s *Store) ListAuditEvents(ctx context.Context, f AuditFilter) ([]AuditEvent, error) {
	where := []string{"true"}
	args := []any{}
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, fmt.Sprintf(cond, len(args)))
	}

	if f.Event != "" {
		add("event = $%d", f.Event)
	}
	if f.Outcome != "" {
		add("outcome = $%d", f.Outcome)
	}
	if f.Subject != "" {
		add("subject = $%d", f.Subject)
	}
	if f.Actor != "" {
		add("actor = $%d", f.Actor)
	}
	if !f.Since.IsZero() {
		add("created_at >= $%d", f.Since)
	}
	if !f.Until.IsZero() {
		add("created_at < $%d", f.Until)
	}
	if f.Before > 0 {
		add("id < $%d", f.Before)
	}

	limit := f.Limit
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(
		`SELECT id, created_at, event, outcome, actor, subject, client_id, ip, user_agent, detail
		 FROM auth_audit_log WHERE %s ORDER BY id DESC LIMIT $%d`, strings.Join(where, " AND "), len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	events := []AuditEvent{}
	for rows.Next() {
		var ev AuditEvent
		var detail sql.NullString
		if err := rows.Scan(&ev.ID, &ev.CreatedAt, &ev.Event, &ev.Outcome, &ev.Actor, &ev.Subject, &ev.ClientID, &ev.IP, &ev.UserAgent, &detail); err != nil {
			return nil, err
		}
		if detail.Valid && detail.String != "" {
			_ = json.Unmarshal([]byte(detail.String), &ev.Detail)
		}
		events = append(events, ev)
	}

	return events, rows.Err()
}

// PruneAuditEvents deletes events older than the retention in one batch.
func (s *Store) PruneAuditEvents(ctx context.Context, olderThan time.Time, limit int) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM auth_audit_log WHERE id IN (
		   SELECT id FROM auth_audit_log WHERE created_at < $1 ORDER BY id LIMIT $2 FOR UPDATE SKIP LOCKED)`,
		olderThan, limit)
	if err != nil {
		return 0, err
	}

	return res.RowsAffected()
}

func (m *Auth) pruneAuditEvents(ctx context.Context) {
	cutoff := time.Now().Add(-m.cache.Snapshot().Audit.GetRetention())
	for {
		deleted, err := m.store.PruneAuditEvents(ctx, cutoff, flowCleanupBatch)
		if err != nil {
			slog.Error("auth audit cleanup failed", "error", err.Error())

			return
		}
		if deleted < flowCleanupBatch {
			return
		}
	}
}

// ListAuditAPI serves GET /v1/audit with filters event, outcome, subject,
// actor, since, until (RFC 3339), before (id) and limit.
func (m *Auth) ListAuditAPI(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := AuditFilter{
		Event:   q.Get("event"),
		Outcome: q.Get("outcome"),
		Subject: q.Get("subject"),
		Actor:   q.Get("actor"),
	}

	for name, dst := range map[string]*time.Time{"since": &f.Since, "until": &f.Until} {
		if v := q.Get(name); v != "" {
			t, err := time.Parse(time.RFC3339, v)
			if err != nil {
				httputil.HandleError(w, httputil.NewError(name+" must be RFC 3339", err, http.StatusBadRequest))

				return
			}
			*dst = t
		}
	}
	if v := q.Get("before"); v != "" {
		f.Before, _ = strconv.ParseInt(v, 10, 64)
	}
	if v := q.Get("limit"); v != "" {
		f.Limit, _ = strconv.Atoi(v)
	}

	events, err := m.store.ListAuditEvents(r.Context(), f)
	if err != nil {
		httputil.HandleError(w, httputil.NewError("cannot list audit events", err, http.StatusInternalServerError))

		return
	}

	httputil.JSON(w, http.StatusOK, Response[[]AuditEvent]{Payload: events})
}

// auditWrites wraps the management API: every successful write records an
// admin.change event with the method and path.
func (m *Auth) auditWrites(next http.Handler) http.Handler {
	prefix := strings.TrimSuffix(m.PrefixPath, "/") + "/v1/"

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions ||
			!strings.HasPrefix(httputil.CleanPath(r.URL.Path), prefix) ||
			strings.HasPrefix(httputil.CleanPath(r.URL.Path), prefix+"check") {
			next.ServeHTTP(w, r)

			return
		}

		rec := &statusRecorder{ResponseWriter: w, code: http.StatusOK}
		next.ServeHTTP(rec, r)

		outcome := auditSuccess
		if rec.code >= 400 {
			outcome = auditFailure
		}
		// unauthenticated noise is not an admin change
		if rec.code == http.StatusUnauthorized || rec.code == http.StatusForbidden || rec.code == http.StatusNotFound {
			if requestPrincipal(r) == "" {
				return
			}
		}

		m.audit(r, AuditEvent{
			Event:   auditAdminChange,
			Outcome: outcome,
			Detail:  map[string]any{"method": r.Method, "path": r.URL.Path, "status": rec.code},
		})
	})
}

type statusRecorder struct {
	http.ResponseWriter
	code    int
	written bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if !s.written {
		s.code = code
		s.written = true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	s.written = true

	return s.ResponseWriter.Write(b)
}

func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// auditBodyRecorder keeps the response body of the token endpoint so the
// audit record can name the subject of an issued token.
type auditBodyRecorder struct {
	statusRecorder
	body bytes.Buffer
}

func (a *auditBodyRecorder) Write(b []byte) (int, error) {
	if a.body.Len() < 64<<10 {
		a.body.Write(b)
	}

	return a.statusRecorder.Write(b)
}

// APITokenAudited is the token endpoint with an audit record per request.
func (m *Auth) APITokenAudited(w http.ResponseWriter, r *http.Request) {
	// read the (bounded) form once for the audit record, then hand an
	// identical body to the token endpoint
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{Error: "invalid_request", ErrorDescription: "request body too large", code: http.StatusBadRequest})

		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))

	form := url.Values{}
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
		form, _ = url.ParseQuery(string(body))
	} else {
		var fields map[string]any
		if json.Unmarshal(body, &fields) == nil {
			for k, v := range fields {
				if s, ok := v.(string); ok {
					form.Set(k, s)
				}
			}
		}
	}

	rec := &auditBodyRecorder{statusRecorder: statusRecorder{ResponseWriter: w, code: http.StatusOK}}
	m.APIToken(rec, r)

	grant := form.Get("grant_type")
	ev := AuditEvent{Event: auditTokenIssue, Outcome: auditSuccess, ClientID: form.Get("client_id")}
	if id, _, ok := r.BasicAuth(); ok {
		ev.ClientID = id
	}

	switch grant {
	case "refresh_token":
		ev.Event = auditTokenRefresh
	case "password", grantTypeEmailCode:
		ev.Event = auditLogin
	}

	detail := map[string]any{"grant_type": grant}

	if rec.code >= 400 {
		ev.Outcome = auditFailure
		var e AccessTokenErrorResponse
		if json.Unmarshal(rec.body.Bytes(), &e) == nil {
			detail["error"] = e.Error
			detail["error_description"] = e.ErrorDescription
			if e.Error == "invalid_client" {
				ev.Event = auditClientAuth
			}
			if e.Error == "mfa_required" {
				// a challenge, not a failure worth alerting on
				ev.Event, ev.Outcome = auditMFA, "challenge"
			}
		}
		if grant == "password" {
			ev.Subject = form.Get("username")
		}
	} else {
		var tok AccessTokenResponse
		if json.Unmarshal(rec.body.Bytes(), &tok) == nil && tok.AccessToken != "" {
			claims := map[string]any{}
			if parts := strings.Split(tok.AccessToken, "."); len(parts) == 3 {
				if raw, err := base64RawURLDecode(parts[1]); err == nil {
					_ = json.Unmarshal(raw, &claims)
				}
			}
			ev.Subject, _ = claims["sub"].(string)
			if sid, _ := claims["sid"].(string); sid != "" {
				detail["sid"] = sid
			}
			detail["token_type"] = tok.TokenType
			detail["scope"] = tok.Scope
		}
	}

	ev.Detail = detail
	m.audit(r, ev)
}

func base64RawURLDecode(v string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(v)
}

// audited records event for every request to handler: success below 400,
// failure otherwise, with the provider path value for federated logins.
// Token responses name their subject; a passkey ceremony "begin" step that
// issues no token is not recorded.
func (m *Auth) audited(event string, handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rec := &auditBodyRecorder{statusRecorder: statusRecorder{ResponseWriter: w, code: http.StatusOK}}
		handler(rec, r)

		ev := AuditEvent{Event: event, Outcome: auditSuccess, Detail: map[string]any{"path": r.URL.Path}}
		if provider := r.PathValue("provider"); provider != "" {
			ev.Detail["provider"] = provider
		}

		if rec.code >= 400 {
			ev.Outcome = auditFailure
			var e AccessTokenErrorResponse
			if json.Unmarshal(rec.body.Bytes(), &e) == nil && e.Error != "" {
				ev.Detail["error"] = e.Error
			}
		} else if rec.code < 300 {
			var tok AccessTokenResponse
			if json.Unmarshal(rec.body.Bytes(), &tok) != nil || tok.AccessToken == "" {
				if event == auditLogin && strings.HasSuffix(r.URL.Path, "/passkey") {
					return
				}
			} else if parts := strings.Split(tok.AccessToken, "."); len(parts) == 3 {
				if raw, err := base64RawURLDecode(parts[1]); err == nil {
					claims := map[string]any{}
					_ = json.Unmarshal(raw, &claims)
					ev.Subject, _ = claims["sub"].(string)
					ev.ClientID, _ = claims["azp"].(string)
				}
			}
		}

		m.audit(r, ev)
	}
}
