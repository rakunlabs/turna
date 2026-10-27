// Package cache stores upstream responses and serves them again following
// HTTP caching rules.
package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dustin/go-humanize"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
)

var defaultStatuses = []int{200, 203, 204, 300, 301, 308, 404, 405, 410, 414, 501}

type Cache struct {
	// TTL of responses without Cache-Control max-age or Expires, default 0
	// does not store them.
	TTL time.Duration `cfg:"ttl"`
	// MaxTTL caps the freshness given by the upstream, 0 is no cap.
	MaxTTL time.Duration `cfg:"max_ttl"`
	// StaleWhileRevalidate serves an expired response while it is refreshed
	// in the background, overridden by the upstream directive.
	StaleWhileRevalidate time.Duration `cfg:"stale_while_revalidate"`
	// StaleIfError serves an expired response when the upstream fails,
	// overridden by the upstream directive.
	StaleIfError time.Duration `cfg:"stale_if_error"`

	// Methods that are cached, default GET and HEAD. HEAD is answered from
	// GET responses.
	Methods []string `cfg:"methods"`
	// Statuses that are cached, default the heuristically cacheable ones.
	Statuses []int `cfg:"statuses"`
	// MaxBodySize is the largest stored response, default 1MB.
	MaxBodySize string `cfg:"max_body_size"`
	// KeyHeaders are request headers added to the key, e.g. a tenant.
	KeyHeaders []string `cfg:"key_headers"`
	// CacheAuthorized stores responses of requests with Authorization even
	// without public; enable only when they do not depend on the user.
	CacheAuthorized bool `cfg:"cache_authorized"`
	// IgnoreRequestCacheControl ignores no-cache of clients, which would
	// otherwise skip the stored response.
	IgnoreRequestCacheControl bool `cfg:"ignore_request_cache_control"`
	// StatusHeader reports HIT, MISS, STALE or BYPASS, default X-Cache;
	// "-" disables it.
	StatusHeader string `cfg:"status_header"`

	// MaxSize of the memory cache, default 64MB.
	MaxSize string `cfg:"max_size"`
	// Redis shares the cache between instances; default is in memory.
	Redis *Redis `cfg:"redis"`

	statuses map[int]bool
}

type entry struct {
	Status int         `json:"s"`
	Header http.Header `json:"h"`
	Body   []byte      `json:"b"`
	Stored time.Time   `json:"t"`
	Fresh  int64       `json:"f"`
	SWR    int64       `json:"w"`
	SIE    int64       `json:"e"`
}

func (e *entry) age(now time.Time) time.Duration {
	return max(now.Sub(e.Stored), 0)
}

func (e *entry) fresh(now time.Time) bool {
	return e.age(now) < time.Duration(e.Fresh)
}

func (e *entry) staleWhileRevalidate(now time.Time) bool {
	return e.age(now) < time.Duration(e.Fresh+e.SWR)
}

func (e *entry) staleIfError(now time.Time) bool {
	return e.age(now) < time.Duration(e.Fresh+e.SIE)
}

type handler struct {
	*Cache

	store       store
	methods     []string
	maxBody     int64
	statusName  string
	keyHeaders  []string
	revalidates sync.Map
	ctx         context.Context
}

func (m *Cache) Middleware(ctx context.Context) (func(http.Handler) http.Handler, error) {
	if m.TTL < 0 || m.MaxTTL < 0 || m.StaleWhileRevalidate < 0 || m.StaleIfError < 0 {
		return nil, errors.New("cache durations must not be negative")
	}

	h := &handler{Cache: m, ctx: ctx}

	h.methods = []string{http.MethodGet, http.MethodHead}
	if len(m.Methods) > 0 {
		h.methods = nil
		for _, method := range m.Methods {
			method = strings.ToUpper(strings.TrimSpace(method))
			if method != http.MethodGet && method != http.MethodHead {
				return nil, fmt.Errorf("cache method %q is not supported, expected GET or HEAD", method)
			}

			h.methods = append(h.methods, method)
		}
	}

	statuses := m.Statuses
	if len(statuses) == 0 {
		statuses = defaultStatuses
	}

	m.statuses = make(map[int]bool, len(statuses))
	for _, s := range statuses {
		if s == http.StatusPartialContent || s < 200 || s > 599 {
			return nil, fmt.Errorf("cache status %d is not cacheable", s)
		}

		m.statuses[s] = true
	}

	var err error
	if h.maxBody, err = parseSize("max_body_size", m.MaxBodySize, "1MB"); err != nil {
		return nil, err
	}

	h.statusName = "X-Cache"
	switch v := strings.TrimSpace(m.StatusHeader); v {
	case "-":
		h.statusName = ""
	case "":
	default:
		h.statusName = v
	}

	for _, k := range m.KeyHeaders {
		h.keyHeaders = append(h.keyHeaders, http.CanonicalHeaderKey(strings.TrimSpace(k)))
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
		return 0, fmt.Errorf("cache %s %q: %w", name, v, err)
	}

	if size == 0 || size > 1<<40 {
		return 0, fmt.Errorf("cache %s %q is out of range", name, v)
	}

	return int64(size), nil
}

// baseKey identifies the resource; Vary headers select a variant of it.
func (h *handler) baseKey(r *http.Request) string {
	var b strings.Builder

	b.WriteString(httputil.Scheme(r))
	b.WriteByte(0)
	b.WriteString(r.Host)
	b.WriteByte(0)
	b.WriteString(r.URL.RequestURI())

	for _, k := range h.keyHeaders {
		b.WriteByte(0)
		b.WriteString(strings.Join(r.Header.Values(k), ","))
	}

	sum := sha256.Sum256([]byte(b.String()))

	return hex.EncodeToString(sum[:])
}

func variantKey(base string, vary []string, r *http.Request) string {
	if len(vary) == 0 {
		return "e:" + base
	}

	var b strings.Builder
	b.WriteString(base)

	for _, k := range vary {
		b.WriteByte(0)
		b.WriteString(strings.Join(r.Header.Values(k), ","))
	}

	sum := sha256.Sum256([]byte(b.String()))

	return "e:" + hex.EncodeToString(sum[:])
}

// varyOf returns the sorted, canonical Vary header names of a response.
func varyOf(h http.Header) []string {
	var names []string

	for _, v := range h.Values("Vary") {
		for _, name := range strings.Split(v, ",") {
			if name = http.CanonicalHeaderKey(strings.TrimSpace(name)); name != "" && !slices.Contains(names, name) {
				names = append(names, name)
			}
		}
	}

	slices.Sort(names)

	return names
}

func (h *handler) lookup(ctx context.Context, r *http.Request, base string) (*entry, error) {
	raw, ok, err := h.store.get(ctx, "v:"+base)
	if err != nil || !ok {
		return nil, err
	}

	var vary []string
	if err := json.Unmarshal(raw, &vary); err != nil {
		return nil, nil //nolint:nilerr // a broken entry is a miss
	}

	raw, ok, err = h.store.get(ctx, variantKey(base, vary, r))
	if err != nil || !ok {
		return nil, err
	}

	e := &entry{}
	if err := json.Unmarshal(raw, e); err != nil {
		return nil, nil //nolint:nilerr // a broken entry is a miss
	}

	return e, nil
}

func (h *handler) save(ctx context.Context, r *http.Request, base string, e *entry, l lifetime) {
	vary := varyOf(e.Header)

	varyRaw, _ := json.Marshal(vary)
	raw, err := json.Marshal(e)
	if err != nil {
		return
	}

	ttl := l.total()

	// the index lives as long as the longest variant may
	if err := h.store.set(ctx, "v:"+base, varyRaw, max(ttl, time.Hour)); err != nil {
		slog.Warn("cache store failed", "error", err)

		return
	}

	if err := h.store.set(ctx, variantKey(base, vary, r), raw, ttl); err != nil {
		slog.Warn("cache store failed", "error", err)
	}
}

func (h *handler) invalidate(ctx context.Context, r *http.Request) {
	if err := h.store.del(ctx, "v:"+h.baseKey(r)); err != nil {
		slog.Warn("cache invalidate failed", "error", err)
	}
}

func (h *handler) setStatus(w http.ResponseWriter, v string) {
	if h.statusName != "" {
		w.Header().Set(h.statusName, v)
	}
}

func (h *handler) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !slices.Contains(h.methods, r.Method) {
			if isUnsafe(r.Method) {
				sw := &statusRecorder{ResponseWriter: w}
				next.ServeHTTP(sw, r)

				if sw.code < 400 {
					h.invalidate(r.Context(), r)
				}

				return
			}

			next.ServeHTTP(w, r)

			return
		}

		if httputil.IsWebSocket(r) || r.Header.Get("Range") != "" {
			h.setStatus(w, "BYPASS")
			next.ServeHTTP(w, r)

			return
		}

		base := h.baseKey(r)
		now := time.Now()

		skipRead := !h.IgnoreRequestCacheControl && requestNoCache(r)

		var cached *entry
		if !skipRead {
			var err error
			if cached, err = h.lookup(r.Context(), r, base); err != nil {
				slog.Warn("cache lookup failed", "error", err)
			}
		}

		if cached != nil {
			switch {
			case cached.fresh(now):
				h.setStatus(w, "HIT")
				h.serve(w, r, cached, now)

				return
			case cached.staleWhileRevalidate(now):
				h.setStatus(w, "STALE")
				h.serve(w, r, cached, now)
				h.revalidate(next, r, base)

				return
			}
		}

		if r.Method == http.MethodHead {
			// the body is needed to store the response
			h.setStatus(w, "MISS")
			next.ServeHTTP(w, r)

			return
		}

		if cached != nil && cached.staleIfError(now) && int64(len(cached.Body)) <= h.maxBody {
			// hold the response back so it can be replaced on error; the
			// stored body fits max_body_size, so a new one likely does too
			rec := newBufferedWriter()
			req, failed := httputil.WithUpstreamFailure(r)
			next.ServeHTTP(rec, req)

			if failed.Load() || rec.code >= 500 {
				h.setStatus(w, "STALE")
				h.serve(w, r, cached, now)

				return
			}

			h.setStatus(rec, "MISS")
			rec.copyTo(w)
			h.storeResponse(r, base, rec.code, rec.header, rec.body.Bytes(), now)

			return
		}

		h.setStatus(w, "MISS")

		tw := &teeWriter{ResponseWriter: w, limit: h.maxBody}
		next.ServeHTTP(tw, r)

		if tw.overflow || tw.hijacked || r.Context().Err() != nil {
			return
		}

		code := tw.code
		if code == 0 {
			// nothing was written, net/http sends an empty 200
			code = http.StatusOK
			tw.snapshot = w.Header().Clone()
		}

		h.storeResponse(r, base, code, tw.snapshot, tw.body.Bytes(), now)
	})
}

func (h *handler) storeResponse(r *http.Request, base string, code int, header http.Header, body []byte, now time.Time) {
	if header == nil || int64(len(body)) > h.maxBody {
		return
	}

	l, ok := h.lifetime(r, code, header)
	if !ok {
		return
	}

	stored := make(http.Header, len(header))
	for k, v := range header {
		if !skipHeaders[k] && k != h.statusName {
			stored[k] = v
		}
	}

	h.save(r.Context(), r, base, &entry{
		Status: code,
		Header: stored,
		Body:   body,
		Stored: now,
		Fresh:  int64(l.fresh),
		SWR:    int64(l.swr),
		SIE:    int64(l.sie),
	}, l)
}

// revalidate refreshes an entry in the background, once per key at a time.
func (h *handler) revalidate(next http.Handler, r *http.Request, base string) {
	if _, running := h.revalidates.LoadOrStore(base, struct{}{}); running {
		return
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), time.Minute)
	req := r.Clone(ctx)
	req.Body = http.NoBody
	req.Method = http.MethodGet
	req.Header.Del("If-None-Match")
	req.Header.Del("If-Modified-Since")

	go func() {
		defer h.revalidates.Delete(base)
		defer cancel()

		select {
		case <-h.ctx.Done():
			return
		default:
		}

		rec := newBufferedWriter()
		next.ServeHTTP(rec, req)

		if rec.code == 0 {
			rec.code = http.StatusOK
		}

		if rec.code >= 500 {
			return
		}

		h.storeResponse(req, base, rec.code, rec.header, rec.body.Bytes(), time.Now())
	}()
}

func (h *handler) serve(w http.ResponseWriter, r *http.Request, e *entry, now time.Time) {
	dst := w.Header()
	for k, v := range e.Header {
		dst[k] = slices.Clone(v)
	}

	dst.Set("Age", strconv.FormatInt(int64(e.age(now)/time.Second), 10))

	if e.Status == http.StatusOK && notModified(r, e.Header) {
		for _, k := range []string{"Content-Length", "Content-Type", "Content-Encoding"} {
			dst.Del(k)
		}

		w.WriteHeader(http.StatusNotModified)

		return
	}

	dst.Set("Content-Length", strconv.Itoa(len(e.Body)))
	w.WriteHeader(e.Status)

	if r.Method != http.MethodHead {
		_, _ = w.Write(e.Body)
	}
}

func notModified(r *http.Request, h http.Header) bool {
	if inm := r.Header.Get("If-None-Match"); inm != "" {
		etag := strings.TrimPrefix(h.Get("ETag"), "W/")
		if etag == "" {
			return false
		}

		for _, t := range strings.Split(inm, ",") {
			t = strings.TrimSpace(t)
			if t == "*" || strings.TrimPrefix(t, "W/") == etag {
				return true
			}
		}

		return false
	}

	if ims := r.Header.Get("If-Modified-Since"); ims != "" {
		since, err := http.ParseTime(ims)
		if err != nil {
			return false
		}

		mod, err := http.ParseTime(h.Get("Last-Modified"))

		return err == nil && !mod.After(since)
	}

	return false
}

func requestNoCache(r *http.Request) bool {
	cc := parseCacheControl(r.Header)
	if cc.has("no-cache") || cc.has("no-store") {
		return true
	}

	if v, ok := cc.seconds("max-age"); ok && v == 0 {
		return true
	}

	return r.Header.Get("Cache-Control") == "" && strings.Contains(strings.ToLower(r.Header.Get("Pragma")), "no-cache")
}

func isUnsafe(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}

	return false
}
