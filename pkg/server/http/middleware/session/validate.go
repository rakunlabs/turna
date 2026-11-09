package session

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/oauth2/claims"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/oauth2/dpop"
)

// DefaultLeeway is the clock skew tolerated for exp, nbf and iat.
const DefaultLeeway = 30 * time.Second

// DefaultIssuerAudience is the audience the in-process auth middleware puts
// into every token it issues.
const DefaultIssuerAudience = "turna-auth"

var (
	ErrTokenType     = errors.New("token type is not an access token")
	ErrTokenIssuer   = errors.New("token issuer is not accepted")
	ErrTokenAudience = errors.New("token audience is not accepted")
	ErrTokenRevoked  = errors.New("token is revoked")

	errRevocationUnavailable = fmt.Errorf("cannot check token revocation: %w", errSessionUnavailable)
)

// InfRevocationChecker is implemented by issuers that keep a revocation list
// for access token jti values and session (sid) families.
type InfRevocationChecker interface {
	TokenClaimsRevoked(ctx context.Context, jti, sid string) (bool, error)
}

// clientIDClaims hold the OAuth client a token was issued to, depending on
// the provider: azp (OIDC, Keycloak), client_id (RFC 9068), cid (Okta) and
// appid (Azure AD v1).
var clientIDClaims = []string{"azp", "client_id", "cid", "appid"}

// BearerToken returns the token of an "Authorization: Bearer <token>" header
// value; the scheme is case-insensitive (RFC 6750). Other schemes return "".
func BearerToken(header string) string {
	scheme, token, ok := strings.Cut(strings.TrimSpace(header), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return ""
	}

	return strings.TrimSpace(token)
}

// AuthorizationToken returns the access token and its scheme ("Bearer" or
// "DPoP", RFC 9449 §7.1) from an Authorization header value.
func AuthorizationToken(header string) (token, scheme string) {
	scheme, token, ok := strings.Cut(strings.TrimSpace(header), " ")
	if !ok {
		return "", ""
	}

	switch {
	case strings.EqualFold(scheme, "Bearer"):
		return strings.TrimSpace(token), "Bearer"
	case strings.EqualFold(scheme, "DPoP"):
		return strings.TrimSpace(token), "DPoP"
	}

	return "", ""
}

// ValidateAccessToken verifies the signature, lifetime, issuer, audience and
// token type of an access token, and optionally its revocation state. The
// claim checks follow the provider whose key verified the signature. It
// returns the claims and the effective provider name (set_provider wins).
func (m *Session) ValidateAccessToken(ctx context.Context, raw string) (*claims.Custom, string, error) {
	keyFunc := m.KeyFuncParser()
	if keyFunc == nil {
		return nil, "", ErrTokenInvalid
	}

	customClaims := &claims.Custom{}
	jwtToken, err := keyFunc.ParseWithClaims(raw, customClaims)
	if err != nil {
		return nil, "", err
	}

	signedBy, _ := jwtToken.Header["provider_name"].(string)
	provider, ok := m.GetProvider(signedBy)
	if !ok {
		return nil, "", fmt.Errorf("provider %q of the signing key not found: %w", signedBy, ErrTokenInvalid)
	}

	if err := checkAccessClaims(ctx, provider, customClaims.Map); err != nil {
		return nil, "", err
	}

	providerName := signedBy
	if m.SetProvider != "" {
		providerName = m.SetProvider
	}

	return customClaims, providerName, nil
}

func checkAccessClaims(ctx context.Context, provider Provider, claimMap map[string]any) error {
	typ, _ := claimMap["typ"].(string)
	var issuer InfIssuer
	if provider.AuthMiddleware != "" {
		issuer = IssuerRegistry.Get(provider.AuthMiddleware)
		// the auth middleware marks every access token explicitly
		if typ != "Bearer" {
			return ErrTokenType
		}
	} else if strings.EqualFold(typ, "Refresh") || strings.EqualFold(typ, "ID") {
		return ErrTokenType
	}

	iss, _ := claimMap["iss"].(string)
	switch {
	case len(provider.Issuer) > 0:
		if !slices.Contains(provider.Issuer, iss) {
			return ErrTokenIssuer
		}
	case issuer != nil:
		if acceptor, ok := issuer.(InfIssuerAcceptor); ok && !acceptor.AcceptsIssuer(iss) {
			return ErrTokenIssuer
		}
	}

	if !provider.SkipAudienceCheck && !audienceAccepted(provider, claimMap) {
		return ErrTokenAudience
	}

	if provider.CheckRevocation && issuer != nil {
		if checker, ok := issuer.(InfRevocationChecker); ok {
			jti, _ := claimMap["jti"].(string)
			sid, _ := claimMap["sid"].(string)
			revoked, err := checker.TokenClaimsRevoked(ctx, jti, sid)
			if err != nil {
				return errors.Join(errRevocationUnavailable, err)
			}
			if revoked {
				return ErrTokenRevoked
			}
		}
	}

	return nil
}

// audienceAccepted applies the provider audience policy:
//   - audience configured: aud must contain one of the values.
//   - in-process issuer: aud must contain DefaultIssuerAudience.
//   - remote provider: aud must contain oauth2.client_id or the token must be
//     issued to it (azp, client_id, cid or appid claim).
func audienceAccepted(provider Provider, claimMap map[string]any) bool {
	aud := claimMap["aud"]

	if len(provider.Audience) > 0 {
		return slices.ContainsFunc(provider.Audience, func(v string) bool {
			return v != "" && audienceContains(aud, v)
		})
	}

	if provider.AuthMiddleware != "" {
		return audienceContains(aud, DefaultIssuerAudience)
	}

	clientID := ""
	if provider.Oauth2 != nil {
		clientID = provider.Oauth2.ClientID
	}
	if clientID == "" {
		return false
	}

	if audienceContains(aud, clientID) {
		return true
	}

	for _, name := range clientIDClaims {
		if v, _ := claimMap[name].(string); v == clientID {
			return true
		}
	}

	return false
}

// warnProviderPolicies logs providers whose tokens can never pass the
// audience check, so a configuration gap shows up at startup instead of as
// unexplained 401 responses.
func warnProviderPolicies(providers map[string]Provider) {
	for name, provider := range providers {
		if provider.AuthMiddleware != "" || provider.SkipAudienceCheck || len(provider.Audience) > 0 {
			continue
		}

		if provider.Oauth2 == nil || provider.Oauth2.CertURL == "" {
			continue
		}

		if provider.Oauth2.ClientID == "" {
			slog.Warn("session: provider has no oauth2.client_id or audience, its tokens are rejected; set audience or skip_audience_check",
				"provider", name)
		}
	}
}

type dpopReplayer interface {
	DPoPReplay(r *http.Request) dpop.ReplayFunc
}

// checkDPoP enforces RFC 9449 §7 for a validated access token: a DPoP-bound
// token needs the DPoP scheme and a proof of the bound key. Issuer-backed
// providers share the issuer's replay cache; others use a local one.
func (m *Session) checkDPoP(r *http.Request, claimMap map[string]any, scheme, token, providerName string) error {
	if dpop.JKT(claimMap) == "" && !strings.EqualFold(scheme, "DPoP") {
		return nil
	}

	replay := m.dpopReplay.Seen
	if provider, ok := m.GetProvider(providerName); ok && provider.AuthMiddleware != "" {
		if shared, ok := IssuerRegistry.Get(provider.AuthMiddleware).(dpopReplayer); ok {
			replay = shared.DPoPReplay(r)
		}
	}

	return dpop.CheckBinding(r, claimMap, scheme, token, dpop.HTU(r, forwardedOrigin(r)), replay)
}

// forwardedOrigin is the external origin of r behind a proxy.
func forwardedOrigin(r *http.Request) string {
	scheme := r.Header.Get("X-Forwarded-Proto")
	if scheme == "" {
		scheme = "http"
		if r.TLS != nil {
			scheme = "https"
		}
	}

	host := r.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = r.Host
	}

	return scheme + "://" + host
}
