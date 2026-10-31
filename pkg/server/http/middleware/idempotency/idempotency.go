// Package idempotency answers repeated requests with the same
// Idempotency-Key with the stored response of the first one.
package idempotency

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/dustin/go-humanize"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/internal/requestkey"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/internal/statuscode"
)

const (
	stateRunning = "running"
	stateDone    = "done"
)

type Idempotency struct {
	// Header holding the key, default Idempotency-Key.
	Header string `cfg:"header"`
	// Methods that use keys, default POST and PATCH.
	Methods []string `cfg:"methods"`
	// Required rejects requests of Methods without a key with 400.
	Required bool `cfg:"required"`
	// TTL of stored responses, default 24h.
	TTL time.Duration `cfg:"ttl"`
	// LockTimeout is how long a key stays locked while its first request
	// runs, default 1m. A request that runs longer may be executed twice.
	LockTimeout time.Duration `cfg:"lock_timeout"`
	// Scope separates keys of different clients: ip, header:<name>,
	// cookie:<name> and others like rate_limit. Default is header:Authorization.
	Scope []string `cfg:"scope"`
	// TrustedProxies are CIDRs whose forwarding headers are used for the ip scope.
	TrustedProxies []string `cfg:"trusted_proxies"`
	// Statuses that are stored, default 200-499 except 408, 409, 425, 429
	// and 499, which can succeed when sent again.
	Statuses []string `cfg:"statuses"`
	// MaxBodySize is the largest request body hashed and response body
	// stored, default 1MB. Larger requests are rejected with 413.
	MaxBodySize string `cfg:"max_body_size"`
	// StatusHeader reports REPLAY on stored responses, default
	// Idempotent-Replayed; "-" disables it.
	StatusHeader string `cfg:"status_header"`

	// MaxSize of the memory store, default 64MB.
	MaxSize string `cfg:"max_size"`
	// Redis shares keys between instances; default is in memory.
	Redis *Redis `cfg:"redis"`
}

// perRequestHeaders belong to one response and are not replayed: cookies
// may be of another user sharing the scope, the others describe the first
// request.
var perRequestHeaders = []string{
	"Set-Cookie", "Date", "X-Request-Id", "Traceparent", "Tracestate", "Server-Timing",
	"Connection", "Keep-Alive", "Transfer-Encoding", "Trailer", "Upgrade",
}

// notStored are statuses that can succeed when sent again: timeouts,
// conflicts, rate limits and clients that went away (499).
var notStored = []string{"408", "409", "425", "429", "499"}

type record struct {
	State string `json:"s"`
	// Token identifies the request holding the lock.
	Token       string      `json:"t,omitempty"`
	Fingerprint string      `json:"f"`
	Status      int         `json:"c,omitempty"`
	Header      http.Header `json:"h,omitempty"`
	Body        []byte      `json:"b,omitempty"`
}

type handler struct {
	header      string
	methods     []string
	required    bool
	ttl         time.Duration
	lockTimeout time.Duration
	scope       func(*http.Request) string
	statuses    statuscode.Set
	retryable   statuscode.Set
	maxBody     int64
	statusName  string
	store       store
}

func (m *Idempotency) Middleware(ctx context.Context) (func(http.Handler) http.Handler, error) {
	h := &handler{
		header:      http.CanonicalHeaderKey(defaultString(m.Header, "Idempotency-Key")),
		required:    m.Required,
		ttl:         m.TTL,
		lockTimeout: m.LockTimeout,
		statusName:  "Idempotent-Replayed",
	}

	if h.ttl == 0 {
		h.ttl = 24 * time.Hour
	}

	if h.lockTimeout == 0 {
		h.lockTimeout = time.Minute
	}

	if h.ttl < 0 || h.lockTimeout < 0 {
		return nil, errors.New("idempotency durations must not be negative")
	}

	h.methods = []string{http.MethodPost, http.MethodPatch}
	if len(m.Methods) > 0 {
		h.methods = nil
		for _, method := range m.Methods {
			h.methods = append(h.methods, strings.ToUpper(strings.TrimSpace(method)))
		}
	}

	scope := m.Scope
	if len(scope) == 0 {
		scope = []string{"header:Authorization"}
	}

	var err error
	if h.scope, err = requestkey.New("idempotency scope", scope, m.TrustedProxies); err != nil {
		return nil, err
	}

	if h.statuses, err = statuscode.Parse("idempotency", m.Statuses, "200-499"); err != nil {
		return nil, err
	}

	if len(m.Statuses) == 0 {
		h.retryable, _ = statuscode.Parse("idempotency", notStored)
	}

	if h.maxBody, err = parseSize("max_body_size", m.MaxBodySize, "1MB"); err != nil {
		return nil, err
	}

	switch v := strings.TrimSpace(m.StatusHeader); v {
	case "-":
		h.statusName = ""
	case "":
	default:
		h.statusName = v
	}

	if m.Redis != nil {
		if h.store, err = newRedisStore(ctx, m.Redis); err != nil {
			return nil, err
		}
	} else {
		maxSize, err := parseSize("max_size", m.MaxSize, "64MB")
		if err != nil {
			return nil, err
		}

		h.store = newMemoryStore(maxSize)
	}

	return h.middleware, nil
}

func parseSize(name, v, def string) (int64, error) {
	if v == "" {
		v = def
	}

	size, err := humanize.ParseBytes(v)
	if err != nil {
		return 0, fmt.Errorf("idempotency %s %q: %w", name, v, err)
	}

	if size == 0 || size > 1<<40 {
		return 0, fmt.Errorf("idempotency %s %q is out of range", name, v)
	}

	return int64(size), nil
}

func defaultString(v, d string) string {
	if v = strings.TrimSpace(v); v != "" {
		return v
	}

	return d
}

func hash(parts ...string) string {
	s := sha256.New()
	for _, p := range parts {
		_, _ = fmt.Fprintf(s, "%d:%s", len(p), p)
	}

	return hex.EncodeToString(s.Sum(nil))
}

func (h *handler) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !slices.Contains(h.methods, r.Method) {
			next.ServeHTTP(w, r)

			return
		}

		key := r.Header.Get(h.header)
		if key == "" {
			if h.required {
				httputil.HandleError(w, httputil.NewError(h.header+" header is required", nil, http.StatusBadRequest))

				return
			}

			next.ServeHTTP(w, r)

			return
		}

		if len(key) > 255 {
			httputil.HandleError(w, httputil.NewError(h.header+" is too long", nil, http.StatusBadRequest))

			return
		}

		body, err := io.ReadAll(io.LimitReader(r.Body, h.maxBody+1))
		if err != nil {
			code := http.StatusBadRequest
			if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
				code = http.StatusRequestEntityTooLarge
			}

			httputil.HandleError(w, httputil.NewError("cannot read body", err, code))

			return
		}

		if int64(len(body)) > h.maxBody {
			httputil.HandleError(w, httputil.NewError("request body is too large for an idempotent request", nil, http.StatusRequestEntityTooLarge))

			return
		}

		_ = r.Body.Close()
		r.Body = io.NopCloser(bytes.NewReader(body))

		storeKey := hash(h.scope(r), key)
		fingerprint := hash(r.Method, r.Host, r.URL.RequestURI(), string(body))

		ctx := r.Context()

		lock, err := json.Marshal(record{State: stateRunning, Token: rand.Text(), Fingerprint: fingerprint})
		if err != nil {
			next.ServeHTTP(w, r)

			return
		}

		acquired, err := h.store.setNX(ctx, storeKey, lock, h.lockTimeout)
		if err != nil {
			slog.Warn("idempotency store unavailable", "error", err)
			next.ServeHTTP(w, r)

			return
		}

		if !acquired {
			h.replay(w, r, storeKey, fingerprint)

			return
		}

		rec := &recorder{ResponseWriter: w, limit: h.maxBody}

		storeCtx := context.WithoutCancel(ctx)

		completed := false
		defer func() {
			if !completed {
				// a panic, a hijacked connection or a response that is not
				// stored must not keep the key locked; a lock that expired
				// and was taken by another request is left alone
				_ = h.store.release(storeCtx, storeKey, lock)
			}
		}()

		next.ServeHTTP(rec, r)

		if !h.storable(ctx, rec) {
			return
		}

		v, err := json.Marshal(record{
			State:       stateDone,
			Fingerprint: fingerprint,
			Status:      rec.status(),
			Header:      replayHeader(rec.header(w)),
			Body:        rec.body.Bytes(),
		})
		if err != nil {
			return
		}

		if err := h.store.replace(storeCtx, storeKey, lock, v, h.ttl); err != nil {
			slog.Warn("idempotency store unavailable", "error", err)

			return
		}

		completed = true
	})
}

// storable reports whether the response is complete and final. A client
// that went away may not have received it, and the upstream answer is then
// unknown, so it is not stored.
func (h *handler) storable(ctx context.Context, rec *recorder) bool {
	if rec.hijacked || rec.overflow || rec.failed || ctx.Err() != nil {
		return false
	}

	code := rec.status()

	return h.statuses.Contains(code) && !h.retryable.Contains(code)
}

func replayHeader(h http.Header) http.Header {
	out := h.Clone()
	for _, k := range perRequestHeaders {
		out.Del(k)
	}

	return out
}

func (h *handler) replay(w http.ResponseWriter, r *http.Request, storeKey, fingerprint string) {
	v, ok, err := h.store.get(r.Context(), storeKey)
	if err != nil {
		httputil.HandleError(w, httputil.NewError("idempotency store unavailable", err, http.StatusServiceUnavailable))

		return
	}

	var rec record
	if !ok || json.Unmarshal(v, &rec) != nil {
		// the first request finished without storing a response in between
		w.Header().Set("Retry-After", "1")
		httputil.HandleError(w, httputil.NewError("request with this "+h.header+" is in progress, retry later", nil, http.StatusConflict))

		return
	}

	if rec.Fingerprint != fingerprint {
		httputil.HandleError(w, httputil.NewError(h.header+" was used with a different request", nil, http.StatusUnprocessableEntity))

		return
	}

	if rec.State != stateDone {
		w.Header().Set("Retry-After", "1")
		httputil.HandleError(w, httputil.NewError("request with this "+h.header+" is in progress, retry later", nil, http.StatusConflict))

		return
	}

	dst := w.Header()
	for k, vs := range rec.Header {
		dst[k] = vs
	}

	if h.statusName != "" {
		dst.Set(h.statusName, "true")
	}

	w.WriteHeader(rec.Status)
	_, _ = w.Write(rec.Body)
}
