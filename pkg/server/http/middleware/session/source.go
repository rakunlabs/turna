package session

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/rakunlabs/ok"
)

// DefaultProviderSourceTTL is the refresh interval for URL provider sources.
const DefaultProviderSourceTTL = 30 * time.Second

// providerSourceFetchTimeout bounds a single URL fetch so a slow identity
// provider cannot stall the request that triggered the refresh.
const providerSourceFetchTimeout = 5 * time.Second

// ProviderSource makes the provider list dynamic: instead of (or on top of)
// the static `provider` map, the session middleware pulls the UI-managed
// provider list of an auth middleware ("session_providers" settings
// namespace). Dynamic providers overlay same-named static ones.
type ProviderSource struct {
	// AuthMiddleware is the name of an in-process auth middleware instance.
	// The provider list is read directly from its cache; changes apply as
	// soon as the auth cache reloads (no HTTP involved).
	AuthMiddleware string `cfg:"auth_middleware"`
	// URL is the session-providers endpoint of a remote auth middleware
	// (e.g. https://idp.example.com/auth/v1/session-providers). The endpoint
	// is admin-protected; use headers to authenticate.
	URL string `cfg:"url"`
	// TTL is the refresh interval for URL sources. Default 30s.
	TTL time.Duration `cfg:"ttl"`
	// Headers added to every URL request (e.g. X-API-Key of an admin
	// principal on the remote auth).
	Headers map[string]string `cfg:"headers"`
	// InsecureSkipVerify disables TLS verification for URL fetches.
	InsecureSkipVerify bool `cfg:"insecure_skip_verify"`

	client *ok.Client `cfg:"-"`
}

func (s *ProviderSource) ttl() time.Duration {
	if s.TTL > 0 {
		return s.TTL
	}

	return DefaultProviderSourceTTL
}

// providerState is the immutable snapshot of the effective provider set and
// everything derived from it. It is swapped atomically on refresh.
type providerState struct {
	// providers is the static map overlaid with the dynamic source
	// (dynamic wins on name conflict).
	providers map[string]Provider
	// keyFunc validates tokens for the merged provider set. Nil when the
	// initial dynamic build failed; callers fall back to the static keyfunc.
	keyFunc InfKeyFuncParser
	// skipPaths are the public path patterns of every issuer referenced by
	// the merged provider set.
	skipPaths []string

	// signature identifies the keyfunc-relevant part of the provider set.
	signature string
	// version is the source version (in-process snapshot version).
	version uint64
	// fetchedAt is the last URL fetch attempt (success or failure).
	fetchedAt time.Time
}

// InitProviderSource validates the provider_source block and prepares the
// HTTP client for URL mode.
func (m *Session) InitProviderSource() error {
	src := m.ProviderSource
	if src == nil {
		return nil
	}

	if (src.AuthMiddleware == "") == (src.URL == "") {
		return fmt.Errorf("provider_source needs exactly one of auth_middleware or url")
	}

	if src.URL != "" {
		client, err := ok.New(
			ok.WithDisableRetry(true),
			ok.WithInsecureSkipVerify(src.InsecureSkipVerify),
			ok.WithLogger(slog.Default()),
		)
		if err != nil {
			return fmt.Errorf("cannot create provider source client: %w", err)
		}

		src.client = client
	}

	return nil
}

// Providers returns the effective provider map: the static config overlaid
// with the dynamic provider_source list when configured. The returned map
// must be treated as read-only.
func (m *Session) Providers() map[string]Provider {
	if m.ProviderSource != nil {
		m.providerRefresh()

		if st := m.dynamic.Load(); st != nil {
			return st.providers
		}
	}

	return m.Provider
}

// GetProvider returns one effective provider by name.
func (m *Session) GetProvider(name string) (Provider, bool) {
	p, ok := m.Providers()[name]

	return p, ok
}

// KeyFuncParser returns the token validation keyfunc for the effective
// provider set; the static keyfunc when no dynamic state exists yet.
func (m *Session) KeyFuncParser() InfKeyFuncParser {
	if m.ProviderSource != nil {
		if st := m.dynamic.Load(); st != nil && st.keyFunc != nil {
			return st.keyFunc
		}
	}

	if m.Action.Token == nil {
		return nil
	}

	return m.Action.Token.keyFunc
}

// providerRefresh brings the dynamic provider state up to date. In-process
// sources are checked by version on every call (an atomic snapshot read);
// URL sources by TTL. It never blocks concurrent requests: whoever gets the
// refresh lock does the work, everyone else keeps the last known state.
func (m *Session) providerRefresh() {
	src := m.ProviderSource
	if src == nil {
		return
	}

	if src.AuthMiddleware != "" {
		m.refreshFromIssuer(src)

		return
	}

	m.refreshFromURL(src)
}

func (m *Session) refreshFromIssuer(src *ProviderSource) {
	issuer := IssuerRegistry.Get(src.AuthMiddleware)
	if issuer == nil {
		return
	}

	sp, ok := issuer.(InfSessionProviders)
	if !ok {
		return
	}

	dynamic, version := sp.SessionProviders()

	st := m.dynamic.Load()
	if st != nil && st.version == version {
		return
	}

	m.dynamicM.Lock()
	defer m.dynamicM.Unlock()

	// double check under the lock
	if st := m.dynamic.Load(); st != nil && st.version == version {
		return
	}

	m.applyDynamic(dynamic, version, time.Now())
}

func (m *Session) refreshFromURL(src *ProviderSource) {
	st := m.dynamic.Load()
	if st != nil && time.Since(st.fetchedAt) < src.ttl() {
		return
	}

	// someone else is refreshing; keep serving the last known state
	if !m.dynamicM.TryLock() {
		return
	}
	defer m.dynamicM.Unlock()

	if st := m.dynamic.Load(); st != nil && time.Since(st.fetchedAt) < src.ttl() {
		return
	}

	dynamic, version, err := fetchProviders(src)
	now := time.Now()
	if err != nil {
		slog.Warn("session: cannot fetch provider source", "url", src.URL, "error", err.Error())

		// back off until the next TTL window; keep the last known providers
		if st := m.dynamic.Load(); st != nil {
			next := *st
			next.fetchedAt = now
			m.dynamic.Store(&next)
		}

		return
	}

	if st := m.dynamic.Load(); st != nil && st.signature != "" && st.version == version && version != 0 {
		next := *st
		next.fetchedAt = now
		m.dynamic.Store(&next)

		return
	}

	m.applyDynamic(dynamic, version, now)
}

// applyDynamic merges the dynamic providers over the static map and swaps in
// a new state. The keyfunc and skip paths are rebuilt only when the
// keyfunc-relevant part of the set changed. Callers must hold dynamicM.
func (m *Session) applyDynamic(dynamic map[string]Provider, version uint64, now time.Time) {
	old := m.dynamic.Load()

	merged := make(map[string]Provider, len(m.Provider)+len(dynamic))
	for k, v := range m.Provider {
		merged[k] = v
	}
	for k, v := range dynamic {
		merged[k] = v
	}

	st := &providerState{
		providers: merged,
		signature: keyFuncSignature(merged),
		version:   version,
		fetchedAt: now,
	}

	if old != nil && old.signature == st.signature {
		st.keyFunc = old.keyFunc
		st.skipPaths = old.skipPaths
		m.dynamic.Store(st)

		return
	}

	keyFunc, err := buildKeyFunc(merged)
	if err != nil {
		slog.Error("session: cannot build keyfunc for dynamic providers", "error", err.Error())

		// keep validating with what we had; the signature is stored so the
		// next configuration change triggers another rebuild attempt.
		if old != nil {
			st.keyFunc = old.keyFunc
			st.skipPaths = old.skipPaths
		}
		m.dynamic.Store(st)

		return
	}

	st.keyFunc = keyFunc
	st.skipPaths = issuerSkipPatternsFor(merged)

	if old != nil && old.keyFunc != nil {
		if closer, ok := old.keyFunc.(interface{ EndBackground() }); ok {
			closer.EndBackground()
		}
	}

	m.dynamic.Store(st)
}

// keyFuncSignature identifies the part of a provider set that the keyfunc
// and issuer skip paths are built from.
func keyFuncSignature(providers map[string]Provider) string {
	parts := make([]string, 0, len(providers))
	for name, provider := range providers {
		certURL := ""
		if provider.Oauth2 != nil {
			certURL = provider.Oauth2.CertURL
		}

		parts = append(parts, name+"\x00"+provider.AuthMiddleware+"\x00"+certURL)
	}

	sort.Strings(parts)

	return strings.Join(parts, "\x01")
}

// providersResponse is the shape of the auth middleware's
// GET /v1/session-providers endpoint.
type providersResponse struct {
	Payload map[string]Provider `json:"payload"`
	Meta    struct {
		Version uint64 `json:"version"`
	} `json:"meta"`
}

func fetchProviders(src *ProviderSource) (map[string]Provider, uint64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), providerSourceFetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src.URL, nil)
	if err != nil {
		return nil, 0, err
	}

	req.Header.Set("Accept", "application/json")
	for k, v := range src.Headers {
		req.Header.Set(k, v)
	}

	resp, err := src.client.HTTP.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, 0, err
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, 0, fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var parsed providersResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, 0, fmt.Errorf("cannot parse provider source response: %w", err)
	}

	return parsed.Payload, parsed.Meta.Version, nil
}
