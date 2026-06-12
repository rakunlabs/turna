package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/crewjam/saml"
	"github.com/crewjam/saml/samlsp"
	dsig "github.com/russellhaering/goxmldsig"
)

const samlSettingNamespace = "saml"

// samlSetting is the encrypted "saml" setting namespace carrying the
// service provider signing key pair, generated on first use.
type samlSetting struct {
	Certificate string `json:"certificate"`
	PrivateKey  string `json:"private_key"`
}

// SAMLProviderConfig is the decoded SAML provider config stored in
// auth_saml_providers.
type SAMLProviderConfig struct {
	// MetadataURL of the IdP; fetched and cached.
	MetadataURL string `json:"metadata_url"`
	// MetadataXML inline IdP metadata; takes precedence over metadata_url.
	MetadataXML string `json:"metadata_xml"`
	// EntityID of this SP. Default is the metadata URL of the provider.
	EntityID string `json:"entity_id"`
	// AliasAttribute to read the user alias from; default tries
	// email-like attributes and falls back to the subject NameID.
	AliasAttribute string `json:"alias_attribute"`
	// SignRequests signs AuthnRequests with the SP key (RSA-SHA256).
	SignRequests bool `json:"sign_requests"`

	// ClaimMapping maps assertion attributes to local users and roles;
	// roles_claim matches the attribute name or friendly name.
	ClaimMapping ClaimMapping `json:"claim_mapping"`
}

// samlManager caches fetched IdP metadata per provider.
type samlManager struct {
	m        sync.Mutex
	metadata map[string]*samlMetadataEntry
}

type samlMetadataEntry struct {
	// source identifies the metadata origin (inline xml or url).
	source    string
	descrip   *saml.EntityDescriptor
	fetchedAt time.Time
}

const samlMetadataTTL = time.Hour

// samlRelay is the payload stored in auth_flow_codes for a SAML login.
type samlRelay struct {
	Provider    string   `json:"provider"`
	RedirectURI string   `json:"redirect_uri"`
	OrgState    string   `json:"org_state"`
	Scope       []string `json:"scope"`
	RequestID   string   `json:"request_id"`
	// PKCE challenge carried into the issued local code.
	CodeChallenge       string `json:"code_challenge,omitempty"`
	CodeChallengeMethod string `json:"code_challenge_method,omitempty"`
}

const samlRelayTTL = 10 * time.Minute

// generateSAMLSetting creates and persists a self-signed SP key pair.
func (m *Auth) generateSAMLSetting(ctx context.Context, updatedBy string) (samlSetting, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return samlSetting{}, fmt.Errorf("generate rsa key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return samlSetting{}, err
	}

	template := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "turna-auth-saml"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(10, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}

	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return samlSetting{}, fmt.Errorf("create saml certificate: %w", err)
	}

	privateDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return samlSetting{}, err
	}

	setting := samlSetting{
		Certificate: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		PrivateKey:  string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER})),
	}

	settingRaw, err := json.Marshal(setting)
	if err != nil {
		return samlSetting{}, err
	}

	if _, err := m.store.PutSetting(ctx, samlSettingNamespace, settingRaw, updatedBy); err != nil {
		return samlSetting{}, fmt.Errorf("save saml setting: %w", err)
	}

	return setting, nil
}

// samlKeyPair loads (or generates) the SP signing key pair.
func (m *Auth) samlKeyPair(ctx context.Context) (*rsa.PrivateKey, *x509.Certificate, error) {
	setting := m.cache.Snapshot().SAMLKey

	if setting.PrivateKey == "" {
		generated, err := m.generateSAMLSetting(ctx, "system")
		if err != nil {
			return nil, nil, err
		}

		setting = generated

		if err := m.cache.Reload(ctx); err != nil {
			return nil, nil, fmt.Errorf("reload after saml generate: %w", err)
		}
	}

	keyBlock, _ := pem.Decode([]byte(setting.PrivateKey))
	if keyBlock == nil {
		return nil, nil, errors.New("invalid saml private key pem")
	}

	keyAny, err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("parse saml private key: %w", err)
	}

	privateKey, ok := keyAny.(*rsa.PrivateKey)
	if !ok {
		return nil, nil, errors.New("saml private key must be rsa")
	}

	certBlock, _ := pem.Decode([]byte(setting.Certificate))
	if certBlock == nil {
		return nil, nil, errors.New("invalid saml certificate pem")
	}

	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("parse saml certificate: %w", err)
	}

	return privateKey, cert, nil
}

// samlIDPMetadata returns the IdP metadata for a provider, cached for an hour.
func (m *Auth) samlIDPMetadata(ctx context.Context, name string, cfg SAMLProviderConfig) (*saml.EntityDescriptor, error) {
	m.samlM.m.Lock()
	defer m.samlM.m.Unlock()

	if m.samlM.metadata == nil {
		m.samlM.metadata = map[string]*samlMetadataEntry{}
	}

	source := cfg.MetadataXML + "\x00" + cfg.MetadataURL

	if entry, ok := m.samlM.metadata[name]; ok {
		if entry.source == source && (cfg.MetadataXML != "" || time.Since(entry.fetchedAt) < samlMetadataTTL) {
			return entry.descrip, nil
		}
	}

	var descriptor *saml.EntityDescriptor
	var err error

	switch {
	case cfg.MetadataXML != "":
		descriptor, err = samlsp.ParseMetadata([]byte(cfg.MetadataXML))
	case cfg.MetadataURL != "":
		var metadataURL *url.URL
		metadataURL, err = url.Parse(cfg.MetadataURL)
		if err == nil {
			descriptor, err = samlsp.FetchMetadata(ctx, http.DefaultClient, *metadataURL)
		}
	default:
		err = errors.New("provider has no metadata_url or metadata_xml")
	}
	if err != nil {
		return nil, fmt.Errorf("idp metadata: %w", err)
	}

	m.samlM.metadata[name] = &samlMetadataEntry{source: source, descrip: descriptor, fetchedAt: time.Now()}

	return descriptor, nil
}

// samlBaseURL derives the external base url of this middleware from the request.
func (m *Auth) samlBaseURL(r *http.Request) string {
	scheme := r.Header.Get("X-Forwarded-Proto")
	host := r.Header.Get("X-Forwarded-Host")

	if host == "" {
		host = r.Host
	}
	if scheme == "" {
		if r.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}

	return scheme + "://" + host + m.PrefixPath
}

// samlServiceProvider builds the SP for a provider using request-derived URLs.
func (m *Auth) samlServiceProvider(r *http.Request, name string) (*saml.ServiceProvider, *SAMLProviderConfig, error) {
	cfg, ok := m.cache.Snapshot().SAMLProviders[name]
	if !ok {
		return nil, nil, fmt.Errorf("saml provider %q not found", name)
	}

	idpMetadata, err := m.samlIDPMetadata(r.Context(), name, cfg)
	if err != nil {
		return nil, nil, err
	}

	privateKey, cert, err := m.samlKeyPair(r.Context())
	if err != nil {
		return nil, nil, err
	}

	base := m.samlBaseURL(r) + "/saml/" + name

	metadataURL, err := url.Parse(base + "/metadata")
	if err != nil {
		return nil, nil, err
	}

	acsURL, err := url.Parse(base + "/acs")
	if err != nil {
		return nil, nil, err
	}

	sp := &saml.ServiceProvider{
		EntityID:    cfg.EntityID,
		Key:         privateKey,
		Certificate: cert,
		MetadataURL: *metadataURL,
		AcsURL:      *acsURL,
		IDPMetadata: idpMetadata,
	}

	if cfg.SignRequests {
		sp.SignatureMethod = dsig.RSASHA256SignatureMethod
	}

	return sp, &cfg, nil
}

// samlClaims flattens assertion attributes into a claims-like map keyed by
// attribute name and friendly name, so ClaimMapping works uniformly.
func samlClaims(assertion *saml.Assertion) map[string]any {
	claims := map[string]any{}

	for _, statement := range assertion.AttributeStatements {
		for _, attr := range statement.Attributes {
			values := make([]any, 0, len(attr.Values))
			for _, value := range attr.Values {
				if value.Value != "" {
					values = append(values, value.Value)
				}
			}

			if len(values) == 0 {
				continue
			}

			if attr.Name != "" {
				claims[attr.Name] = values
			}
			if attr.FriendlyName != "" {
				claims[attr.FriendlyName] = values
			}
		}
	}

	if assertion.Subject != nil && assertion.Subject.NameID != nil && assertion.Subject.NameID.Value != "" {
		claims["name_id"] = assertion.Subject.NameID.Value
	}

	return claims
}

// samlAlias extracts the user alias from the assertion.
func samlAlias(assertion *saml.Assertion, aliasAttribute string) string {
	attrNames := []string{"email", "mail", "emailaddress",
		"http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress",
		"urn:oid:0.9.2342.19200300.100.1.3"}
	if aliasAttribute != "" {
		attrNames = []string{aliasAttribute}
	}

	for _, statement := range assertion.AttributeStatements {
		for _, attr := range statement.Attributes {
			for _, want := range attrNames {
				if !strings.EqualFold(attr.Name, want) && !strings.EqualFold(attr.FriendlyName, want) {
					continue
				}

				for _, value := range attr.Values {
					if value.Value != "" {
						return value.Value
					}
				}
			}
		}
	}

	if aliasAttribute == "" && assertion.Subject != nil && assertion.Subject.NameID != nil {
		return assertion.Subject.NameID.Value
	}

	return ""
}
