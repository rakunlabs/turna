// Package jwks fetches remote JWK Sets and resolves JWT signing keys by kid.
//
// It is a minimal in-repo replacement for github.com/MicahParks/keyfunc/v2,
// covering only what turna needs: synchronous initial fetch, optional
// background refresh, and kid-based key lookup compatible with
// github.com/golang-jwt/jwt/v5's jwt.Keyfunc.
package jwks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var (
	// ErrKIDNotFound is returned by Keyfunc when the token's kid is not found
	// in the JWK Set. Callers rely on errors.Is for fall-through logic.
	ErrKIDNotFound = errors.New("kid not found in JWKS")
	// ErrJWKAlgMismatch is returned when the JWK's alg does not match the
	// token's alg header.
	ErrJWKAlgMismatch = errors.New("JWK alg does not match token alg")
)

// Options configures Get.
type Options struct {
	// Ctx cancels the background refresh goroutine. Defaults to context.Background().
	Ctx context.Context
	// Client is used to fetch the JWK Set. Defaults to http.DefaultClient.
	Client *http.Client
	// RefreshInterval enables background refresh when > 0.
	RefreshInterval time.Duration
	// RefreshErrorHandler is called when a background refresh fails.
	RefreshErrorHandler func(err error)
}

type parsedKey struct {
	key any
	alg string
}

// JWKS holds the keys of a remote JWK Set and refreshes them in background.
type JWKS struct {
	url    string
	client *http.Client
	cancel context.CancelFunc

	mu   sync.RWMutex
	keys map[string]parsedKey
}

// Get fetches the JWK Set from the given URL. The initial fetch is synchronous;
// an error is returned if it fails. If opts.RefreshInterval > 0, a background
// goroutine refreshes the keys until opts.Ctx is canceled or EndBackground is
// called.
func Get(jwksURL string, opts Options) (*JWKS, error) {
	ctx := opts.Ctx
	if ctx == nil {
		ctx = context.Background()
	}

	client := opts.Client
	if client == nil {
		client = http.DefaultClient
	}

	j := &JWKS{
		url:    jwksURL,
		client: client,
	}

	if err := j.refresh(ctx); err != nil {
		return nil, err
	}

	if opts.RefreshInterval > 0 {
		ctx, cancel := context.WithCancel(ctx)
		j.cancel = cancel

		go j.backgroundRefresh(ctx, opts.RefreshInterval, opts.RefreshErrorHandler)
	}

	return j, nil
}

// EndBackground stops the background refresh goroutine, if any.
func (j *JWKS) EndBackground() {
	if j.cancel != nil {
		j.cancel()
	}
}

// Keyfunc matches the signature of github.com/golang-jwt/jwt/v5's jwt.Keyfunc.
// It returns the public key for the token's kid header, ErrKIDNotFound if the
// kid is unknown, or ErrJWKAlgMismatch if the JWK declares a different alg.
func (j *JWKS) Keyfunc(token *jwt.Token) (any, error) {
	kid, ok := token.Header["kid"].(string)
	if !ok {
		return nil, fmt.Errorf("token has no kid header: %w", ErrKIDNotFound)
	}

	j.mu.RLock()
	k, found := j.keys[kid]
	j.mu.RUnlock()

	if !found {
		return nil, fmt.Errorf("kid %q: %w", kid, ErrKIDNotFound)
	}

	if k.alg != "" {
		if alg, _ := token.Header["alg"].(string); alg != "" && alg != k.alg {
			return nil, fmt.Errorf("kid %q has alg %q, token alg %q: %w", kid, k.alg, alg, ErrJWKAlgMismatch)
		}
	}

	return k.key, nil
}

// Len returns the number of usable keys currently held.
func (j *JWKS) Len() int {
	j.mu.RLock()
	defer j.mu.RUnlock()

	return len(j.keys)
}

func (j *JWKS) backgroundRefresh(ctx context.Context, interval time.Duration, errHandler func(err error)) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := j.refresh(ctx); err != nil {
				if errHandler != nil {
					errHandler(err)
				}
			}
		}
	}
}

func (j *JWKS) refresh(ctx context.Context) error {
	keys, err := fetch(ctx, j.client, j.url)
	if err != nil {
		return err
	}

	j.mu.Lock()
	j.keys = keys
	j.mu.Unlock()

	return nil
}

func fetch(ctx context.Context, client *http.Client, jwksURL string) (map[string]parsedKey, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jwksURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create JWKS request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch JWKS from %q: %w", jwksURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch JWKS from %q: status %d", jwksURL, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read JWKS response from %q: %w", jwksURL, err)
	}

	var raw rawJWKS
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("failed to decode JWKS from %q: %w", jwksURL, err)
	}

	keys := make(map[string]parsedKey, len(raw.Keys))
	for _, k := range raw.Keys {
		// only signature keys; keys without "use" are accepted
		if k.Use != "" && k.Use != "sig" {
			continue
		}

		key, err := parseJWK(k)
		if err != nil {
			// skip unusable keys instead of failing the whole set
			continue
		}

		keys[k.KID] = parsedKey{key: key, alg: k.Alg}
	}

	return keys, nil
}
