package dnspath

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"regexp"
	"sync"
	"time"

	httputil2 "github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/worldline-go/klient"
)

var (
	DefaultDuration = 10 * time.Second
	DefaultSchema   = "http"
)

// DNSPath is a middleware that replaces the path of the request with hostname to IP address.
//   - http://proxy/metrics/kafka/1/metrics -> http://10.0.0.1/metrics
//   - http://proxy/metrics/kafka/2/metrics -> http://10.0.0.2/metrics
type DNSPath struct {
	Paths []*Path `cfg:"paths"`
}

type Path struct {
	DNS string `cfg:"dns"`
	// Regex to match the path
	// Example: /metrics/kafka/(\d+)/(.*)
	Regex string `cfg:"regex"`
	// Number to extract in regex
	// Example: $1
	Number      string `cfg:"number"`
	Replacement string `cfg:"replacement"`

	Schema             string        `cfg:"schema"`
	InsecureSkipVerify bool          `cfg:"insecure_skip_verify"`
	Duration           time.Duration `cfg:"duration"`

	rgx      *regexp.Regexp
	client   *klient.Client
	ipHolder *IPHolder

	lastCheck time.Time
	m         sync.RWMutex
}

func (p *Path) Check() bool {
	p.m.RLock()
	defer p.m.RUnlock()

	return !p.lastCheck.IsZero() && time.Since(p.lastCheck) < p.Duration
}

func (p *Path) DNSFetch() {
	if p.Check() {
		return
	}

	p.m.Lock()
	defer p.m.Unlock()

	ips, err := net.LookupIP(p.DNS)
	if err != nil {
		slog.Warn("dnsPath cannot resolve dns", slog.String("dns", p.DNS), slog.String("error", err.Error()))
	}

	p.ipHolder.Set(ips)
	p.lastCheck = time.Now()
}

func (m *DNSPath) Middleware() (func(http.Handler) http.Handler, error) {
	for _, p := range m.Paths {
		rgx, err := regexp.Compile(p.Regex)
		if err != nil {
			return nil, fmt.Errorf("dnsPath invalid regex: %s", err)
		}

		p.rgx = rgx
		if p.Schema == "" {
			p.Schema = DefaultSchema
		}

		client, err := klient.NewPlain(
			klient.WithInsecureSkipVerify(p.InsecureSkipVerify),
		)
		if err != nil {
			return nil, fmt.Errorf("cannot create klient: %w", err)
		}

		p.client = client
		p.ipHolder = NewIPHolder()

		if p.Duration == 0 {
			p.Duration = DefaultDuration
		}
	}

	return func(_ http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			client := m.ReplaceRequest(r)

			if r.Host == "" || client == nil {
				httputil2.HandleError(w, httputil2.NewError("cannot find host", nil, http.StatusServiceUnavailable))

				return
			}

			m.ForwardRequest(w, r, client.HTTP.Transport)
		})
	}, nil
}

func (m *DNSPath) ReplaceRequest(r *http.Request) *klient.Client {
	for _, p := range m.Paths {
		if p.rgx.MatchString(r.URL.Path) {
			p.DNSFetch()

			number := p.rgx.ReplaceAllString(r.URL.Path, p.Number)
			r.URL.Path = p.rgx.ReplaceAllString(r.URL.Path, p.Replacement)

			r.Host = p.ipHolder.GetStr(number)
			r.URL.Scheme = p.Schema

			return p.client
		}
	}

	return nil
}

func (m *DNSPath) ForwardRequest(w http.ResponseWriter, r *http.Request, transport http.RoundTripper) {
	proxy := httputil.NewSingleHostReverseProxy(r.URL)
	proxy.Transport = transport

	proxy.ServeHTTP(w, r)
}
