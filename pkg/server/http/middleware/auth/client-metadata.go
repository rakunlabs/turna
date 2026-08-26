package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const clientMetadataMaxSize = 5 * 1024

type clientMetadataDocument struct {
	ClientID                string   `json:"client_id"`
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
}

func validateClientMetadataURL(clientID string) (*url.URL, error) {
	u, err := url.Parse(clientID)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.Path == "" || u.User != nil || u.Fragment != "" {
		return nil, fmt.Errorf("client_id metadata URL must be HTTPS with a path and no credentials or fragment")
	}

	decodedPath, err := url.PathUnescape(u.EscapedPath())
	if err != nil {
		return nil, fmt.Errorf("invalid client_id metadata path: %w", err)
	}
	for _, segment := range strings.Split(decodedPath, "/") {
		if segment == "." || segment == ".." {
			return nil, fmt.Errorf("client_id metadata URL must not contain dot path segments")
		}
	}

	return u, nil
}

func publicClientMetadataHTTPClient() *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}

			ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
			if err != nil {
				return nil, err
			}
			for _, ip := range ips {
				if ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() {
					return dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
				}
			}

			return nil, fmt.Errorf("client metadata host does not resolve to a public address")
		},
	}

	return &http.Client{
		Transport: transport,
		Timeout:   10 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func fetchClientMetadata(ctx context.Context, clientID string) (*AccessClient, error) {
	if _, err := validateClientMetadataURL(clientID); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, clientID, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	res, err := publicClientMetadataHTTPClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch client metadata: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch client metadata: status %d", res.StatusCode)
	}
	mediaType, _, err := mime.ParseMediaType(res.Header.Get("Content-Type"))
	if err != nil || (mediaType != "application/json" && !(strings.HasPrefix(mediaType, "application/") && strings.HasSuffix(mediaType, "+json"))) {
		return nil, fmt.Errorf("client metadata response is not JSON")
	}

	body, err := io.ReadAll(io.LimitReader(res.Body, clientMetadataMaxSize+1))
	if err != nil {
		return nil, err
	}
	if len(body) > clientMetadataMaxSize {
		return nil, fmt.Errorf("client metadata exceeds %d bytes", clientMetadataMaxSize)
	}

	return decodeClientMetadata(clientID, body)
}

func decodeClientMetadata(clientID string, body []byte) (*AccessClient, error) {
	var doc clientMetadataDocument
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("decode client metadata: %w", err)
	}
	if doc.ClientID != clientID || doc.ClientName == "" || len(doc.RedirectURIs) == 0 {
		return nil, fmt.Errorf("client metadata must contain matching client_id, client_name and redirect_uris")
	}
	if doc.TokenEndpointAuthMethod != "" && doc.TokenEndpointAuthMethod != "none" {
		return nil, fmt.Errorf("client metadata authentication method %q is not supported", doc.TokenEndpointAuthMethod)
	}
	for _, uri := range doc.RedirectURIs {
		if err := validateRegistrationRedirectURI(uri); err != nil {
			return nil, err
		}
	}
	for _, grant := range doc.GrantTypes {
		if grant != "authorization_code" && grant != "refresh_token" {
			return nil, fmt.Errorf("client metadata grant_type %q is not supported", grant)
		}
	}
	for _, responseType := range doc.ResponseTypes {
		if responseType != "code" {
			return nil, fmt.Errorf("client metadata response_type %q is not supported", responseType)
		}
	}

	return &AccessClient{
		ClientName:   doc.ClientName,
		RedirectURIs: doc.RedirectURIs,
		Public:       true,
	}, nil
}

// isClientMetadataURL reports whether the client_id is an OAuth Client ID
// Metadata Document URL (HTTPS URL with a path).
func isClientMetadataURL(clientID string) bool {
	_, err := validateClientMetadataURL(clientID)

	return err == nil
}

// overlayStoredClient applies locally stored policy fields of a URL-id
// client record onto the fetched metadata document. Administrators pin
// resources, scope, skip_consent and roles_claim for a metadata client
// (store a record whose id is the metadata URL) without maintaining its
// redirect_uris — identity and redirect targets stay authoritative in the
// live document.
func overlayStoredClient(fetched, stored *AccessClient) *AccessClient {
	if stored == nil {
		return fetched
	}

	if len(stored.Resources) > 0 {
		fetched.Resources = stored.Resources
	}
	if len(stored.Scope) > 0 {
		fetched.Scope = stored.Scope
	}
	if stored.SkipConsent {
		fetched.SkipConsent = true
	}
	if stored.RolesClaim != "" {
		fetched.RolesClaim = stored.RolesClaim
	}

	return fetched
}

// metadataClient resolves a URL client_id: the live metadata document is
// fetched and a same-id stored record overlays its policy fields. When the
// fetch fails, a stored record (carrying its own redirect_uris or
// whitelist_urls) keeps the client usable as a full fallback.
func (m *Auth) metadataClient(ctx context.Context, clientID string) (*AccessClient, error) {
	storedValue, hasStored := m.cache.Snapshot().OAuthClients[clientID]
	stored := &storedValue

	fetched, err := fetchClientMetadata(ctx, clientID)
	if err != nil {
		if hasStored {
			// Metadata clients never authenticate with a shared secret. Do not
			// silently downgrade a stored confidential record when fetch fails.
			if stored.ClientSecret != "" {
				return nil, fmt.Errorf("stored metadata client fallback must not contain a client secret")
			}
			stored.Public = true

			return stored, nil
		}

		return nil, err
	}

	if hasStored {
		return overlayStoredClient(fetched, stored), nil
	}

	return fetched, nil
}

func (m *Auth) authorizationRequestClient(ctx context.Context, clientID string) (*AccessClient, error) {
	if isClientMetadataURL(clientID) {
		return m.metadataClient(ctx, clientID)
	}

	if client, ok := m.lookupClient(clientID); ok {
		return client, nil
	}

	return fetchClientMetadata(ctx, clientID)
}

func (m *Auth) authorizationClient(ctx context.Context, clientID, clientSecret string) (*AccessClient, error) {
	if isClientMetadataURL(clientID) {
		if clientSecret != "" {
			return nil, fmt.Errorf("client metadata clients do not use a shared secret")
		}

		return m.metadataClient(ctx, clientID)
	}

	if _, ok := m.lookupClient(clientID); ok {
		return m.resolveClient(clientID, clientSecret)
	}
	if clientSecret != "" {
		return nil, fmt.Errorf("client metadata clients do not use a shared secret")
	}

	return fetchClientMetadata(ctx, clientID)
}
