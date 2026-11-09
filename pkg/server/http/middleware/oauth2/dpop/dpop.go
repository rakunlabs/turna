// Package dpop validates OAuth 2.0 DPoP proofs (RFC 9449).
package dpop

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/rakunlabs/turna/pkg/utils/jwks"
)

const (
	// MaxAge is the oldest accepted proof.
	MaxAge = 5 * time.Minute
	// Skew is the tolerated clock difference.
	Skew = 30 * time.Second
)

// Algorithms are the accepted proof signing algorithms.
var Algorithms = []string{"ES256", "ES384", "ES512", "PS256", "PS384", "PS512", "RS256", "RS384", "RS512", "EdDSA"}

var (
	ErrReplay   = errors.New("dpop proof was already used")
	ErrMissing  = errors.New("exactly one DPoP header is required")
	ErrNotBound = errors.New("access token is not DPoP-bound")
)

// ReplayFunc records a proof id and reports whether it is new. The id is
// unique per key; it must be remembered for at least MaxAge+Skew.
type ReplayFunc func(id string) (bool, error)

// Proof is a validated DPoP proof.
type Proof struct {
	// JKT is the RFC 7638 thumbprint of the proof key (cnf.jkt).
	JKT string
	JTI string
}

type claims struct {
	JTI string `json:"jti"`
	HTM string `json:"htm"`
	HTU string `json:"htu"`
	ATH string `json:"ath,omitempty"`
	jwt.RegisteredClaims
}

// HTU returns the target URI a proof must name for r: scheme and host of
// base (the external origin, e.g. an issuer URL) and the request path.
func HTU(r *http.Request, base string) string {
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		u = &url.URL{Scheme: "https", Host: r.Host}
		if r.TLS == nil {
			u.Scheme = "http"
		}
	}

	return u.Scheme + "://" + u.Host + r.URL.Path
}

func normalize(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}

	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host) + u.EscapedPath()
}

// Verify validates the DPoP header of r for htu. With accessToken set the
// proof must carry its ath hash (resource requests). replay may be nil.
func Verify(r *http.Request, htu, accessToken string, replay ReplayFunc) (*Proof, error) {
	values := r.Header.Values("DPoP")
	if len(values) != 1 || values[0] == "" {
		return nil, ErrMissing
	}

	var jwk *jwks.PublicJWK
	c := &claims{}
	_, err := jwt.ParseWithClaims(values[0], c, func(t *jwt.Token) (any, error) {
		if typ, _ := t.Header["typ"].(string); typ != "dpop+jwt" {
			return nil, errors.New("typ must be dpop+jwt")
		}
		if t.Header["jwk"] == nil {
			return nil, errors.New("jwk header is required")
		}

		raw, err := json.Marshal(t.Header["jwk"])
		if err != nil {
			return nil, err
		}

		jwk, err = jwks.ParsePublicJWK(raw)
		if err != nil {
			return nil, err
		}

		return jwk.Key, nil
	}, jwt.WithValidMethods(Algorithms), jwt.WithIssuedAt(), jwt.WithLeeway(Skew))
	if err != nil {
		return nil, fmt.Errorf("invalid dpop proof: %w", err)
	}

	if c.JTI == "" || c.IssuedAt == nil {
		return nil, errors.New("dpop proof needs jti and iat")
	}
	if age := time.Since(c.IssuedAt.Time); age > MaxAge || age < -Skew {
		return nil, errors.New("dpop proof iat is out of range")
	}
	if c.HTM != r.Method {
		return nil, errors.New("dpop proof htm does not match the request method")
	}
	if normalize(c.HTU) != normalize(htu) {
		return nil, errors.New("dpop proof htu does not match the request uri")
	}

	if accessToken != "" {
		sum := sha256.Sum256([]byte(accessToken))
		if c.ATH != base64.RawURLEncoding.EncodeToString(sum[:]) {
			return nil, errors.New("dpop proof ath does not match the access token")
		}
	}

	if replay != nil {
		fresh, err := replay(jwk.Thumbprint + ":" + c.JTI)
		if err != nil {
			return nil, err
		}
		if !fresh {
			return nil, ErrReplay
		}
	}

	return &Proof{JKT: jwk.Thumbprint, JTI: c.JTI}, nil
}

// JKT returns the cnf.jkt binding of token claims, "" for bearer tokens.
func JKT(tokenClaims map[string]any) string {
	cnf, _ := tokenClaims["cnf"].(map[string]any)
	jkt, _ := cnf["jkt"].(string)

	return jkt
}

// CheckBinding enforces RFC 9449 §7 on a resource request: a DPoP-bound
// token needs the DPoP scheme and a proof of the bound key; the DPoP scheme
// is refused for bearer tokens.
func CheckBinding(r *http.Request, tokenClaims map[string]any, scheme, accessToken, htu string, replay ReplayFunc) error {
	jkt := JKT(tokenClaims)
	if jkt == "" {
		if strings.EqualFold(scheme, "DPoP") {
			return ErrNotBound
		}

		return nil
	}

	if !strings.EqualFold(scheme, "DPoP") {
		return errors.New("DPoP-bound access token requires the DPoP authorization scheme")
	}

	proof, err := Verify(r, htu, accessToken, replay)
	if err != nil {
		return err
	}
	if proof.JKT != jkt {
		return errors.New("dpop proof key does not match the token binding")
	}

	return nil
}

// Challenge is the WWW-Authenticate value for a failed DPoP request.
func Challenge(errCode string) string {
	return fmt.Sprintf(`DPoP error=%q, algs=%q`, errCode, strings.Join(Algorithms, " "))
}

// MemoryReplay is an in-process replay cache for resource servers without a
// shared store; each replica keeps its own window.
type MemoryReplay struct {
	m    sync.Mutex
	seen map[string]time.Time
}

// Seen records id and reports whether it is new.
func (c *MemoryReplay) Seen(id string) (bool, error) {
	c.m.Lock()
	defer c.m.Unlock()

	now := time.Now()
	if c.seen == nil {
		c.seen = make(map[string]time.Time)
	}

	if len(c.seen) > 10000 {
		for k, exp := range c.seen {
			if now.After(exp) {
				delete(c.seen, k)
			}
		}
	}

	if exp, ok := c.seen[id]; ok && now.Before(exp) {
		return false, nil
	}

	c.seen[id] = now.Add(MaxAge + Skew)

	return true, nil
}
