package acme

import (
	"errors"
	"fmt"
	"time"

	"github.com/caddyserver/certmagic"
	"github.com/libdns/cloudflare"
	"github.com/libdns/digitalocean"
	"github.com/libdns/googleclouddns"
	"github.com/libdns/hetzner"
	"github.com/libdns/rfc2136"
	"github.com/libdns/route53"
)

// DNS configures the DNS-01 challenge, required for wildcard certificates.
// Exactly one provider must be set.
type DNS struct {
	Cloudflare   *DNSCloudflare   `cfg:"cloudflare"`
	Route53      *DNSRoute53      `cfg:"route53"`
	GoogleCloud  *DNSGoogleCloud  `cfg:"google_cloud"`
	DigitalOcean *DNSDigitalOcean `cfg:"digitalocean"`
	Hetzner      *DNSHetzner      `cfg:"hetzner"`
	RFC2136      *DNSRFC2136      `cfg:"rfc2136"`

	// TTL of the challenge records.
	TTL time.Duration `cfg:"ttl"`
	// PropagationDelay waits before checking that the record is visible.
	PropagationDelay time.Duration `cfg:"propagation_delay"`
	// PropagationTimeout is the maximum wait for the record, default 2m;
	// -1s disables propagation checks.
	PropagationTimeout time.Duration `cfg:"propagation_timeout"`
	// Resolvers used for propagation checks, e.g. 1.1.1.1:53.
	Resolvers []string `cfg:"resolvers"`
	// OverrideDomain delegates the challenge to another domain (CNAME
	// _acme-challenge.example.com to _acme-challenge.delegated.net).
	OverrideDomain string `cfg:"override_domain"`
}

type DNSCloudflare struct {
	// APIToken needs Zone.DNS:Edit permission.
	APIToken string `cfg:"api_token" log:"-"`
	// ZoneToken is an optional Zone:Read token.
	ZoneToken string `cfg:"zone_token" log:"-"`
}

type DNSRoute53 struct {
	Region          string `cfg:"region"`
	Profile         string `cfg:"profile"`
	AccessKeyID     string `cfg:"access_key_id"`
	SecretAccessKey string `cfg:"secret_access_key" log:"-"`
	SessionToken    string `cfg:"session_token"     log:"-"`
	HostedZoneID    string `cfg:"hosted_zone_id"`
}

type DNSGoogleCloud struct {
	Project string `cfg:"project"`
	// CredentialsFile is a service account JSON file; empty uses
	// Application Default Credentials.
	CredentialsFile string `cfg:"credentials_file"`
}

type DNSDigitalOcean struct {
	APIToken string `cfg:"api_token" log:"-"`
}

type DNSHetzner struct {
	APIToken string `cfg:"api_token" log:"-"`
}

type DNSRFC2136 struct {
	// Server is the DNS server, e.g. ns1.example.com:53.
	Server string `cfg:"server"`
	// KeyName, KeyAlg and Key are the TSIG credentials.
	KeyName string `cfg:"key_name"`
	KeyAlg  string `cfg:"key_alg"`
	Key     string `cfg:"key" log:"-"`
}

func (d *DNS) provider() (certmagic.DNSProvider, error) {
	var (
		providers []certmagic.DNSProvider
		names     []string
	)

	if d.Cloudflare != nil {
		if d.Cloudflare.APIToken == "" {
			return nil, errors.New("dns cloudflare api_token is required")
		}

		providers = append(providers, &cloudflare.Provider{APIToken: d.Cloudflare.APIToken, ZoneToken: d.Cloudflare.ZoneToken})
		names = append(names, "cloudflare")
	}

	if d.Route53 != nil {
		r := d.Route53
		providers = append(providers, &route53.Provider{
			Region:          r.Region,
			Profile:         r.Profile,
			AccessKeyId:     r.AccessKeyID,
			SecretAccessKey: r.SecretAccessKey,
			SessionToken:    r.SessionToken,
			HostedZoneID:    r.HostedZoneID,
		})
		names = append(names, "route53")
	}

	if d.GoogleCloud != nil {
		if d.GoogleCloud.Project == "" {
			return nil, errors.New("dns google_cloud project is required")
		}

		providers = append(providers, &googleclouddns.Provider{Project: d.GoogleCloud.Project, ServiceAccountJSON: d.GoogleCloud.CredentialsFile})
		names = append(names, "google_cloud")
	}

	if d.DigitalOcean != nil {
		if d.DigitalOcean.APIToken == "" {
			return nil, errors.New("dns digitalocean api_token is required")
		}

		providers = append(providers, &digitalocean.Provider{APIToken: d.DigitalOcean.APIToken})
		names = append(names, "digitalocean")
	}

	if d.Hetzner != nil {
		if d.Hetzner.APIToken == "" {
			return nil, errors.New("dns hetzner api_token is required")
		}

		providers = append(providers, &hetzner.Provider{AuthAPIToken: d.Hetzner.APIToken})
		names = append(names, "hetzner")
	}

	if d.RFC2136 != nil {
		r := d.RFC2136
		if r.Server == "" {
			return nil, errors.New("dns rfc2136 server is required")
		}

		providers = append(providers, &rfc2136.Provider{Server: r.Server, KeyName: r.KeyName, KeyAlg: r.KeyAlg, Key: r.Key})
		names = append(names, "rfc2136")
	}

	switch len(providers) {
	case 0:
		return nil, errors.New("dns needs a provider")
	case 1:
		return providers[0], nil
	default:
		return nil, fmt.Errorf("dns needs exactly one provider, got %v", names)
	}
}

func (d *DNS) solver() (*certmagic.DNS01Solver, error) {
	p, err := d.provider()
	if err != nil {
		return nil, err
	}

	return &certmagic.DNS01Solver{
		DNSManager: certmagic.DNSManager{
			DNSProvider:        p,
			TTL:                d.TTL,
			PropagationDelay:   d.PropagationDelay,
			PropagationTimeout: d.PropagationTimeout,
			Resolvers:          d.Resolvers,
			OverrideDomain:     d.OverrideDomain,
		},
	}, nil
}
