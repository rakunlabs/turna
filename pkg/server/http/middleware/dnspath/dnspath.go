package dnspath

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"sync"
	"time"

	httputil2 "github.com/worldline-go/turna/pkg/server/http/httputil"
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
	Port               string        `cfg:"port"`
	InsecureSkipVerify bool          `cfg:"insecure_skip_verify"`
	Duration           time.Duration `cfg:"duration"`

	rgx      *regexp.Regexp
	client   *klient.Client
	ipHolder *IPHolder

	lastCheck time.Time
	m         sync.RWMutex
}

func (p *Path) IsFetched() bool {
	p.m.RLock()
	defer p.m.RUnlock()

	check := !p.lastCheck.IsZero() && time.Since(p.lastCheck) < p.Duration

	return check
}

func (p *Path) DNSFetch() {
	if p.IsFetched() {
		return
	}

	p.m.Lock()
	defer p.m.Unlock()

	ips, err := net.LookupIP(p.DNS)
	if err != nil {
		slog.Warn("dnsPath cannot resolve dns", slog.String("dns", p.DNS), slog.String("error", err.Error()))
	}

	p.ipHolder.Set(ips)

	slog.Debug("dnsPath resolved dns", slog.String("dns", p.DNS), slog.String("ips", p.ipHolder.Dump()))

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
			target, transport := m.ReplaceRequest(r)

			if r.Host == "" || target == nil {
				httputil2.HandleError(w, httputil2.NewError("cannot find host", nil, http.StatusServiceUnavailable))

				return
			}

			m.ForwardRequest(w, r, target, transport)
		})
	}, nil
}

func (m *DNSPath) ReplaceRequest(r *http.Request) (*url.URL, http.RoundTripper) {
	for _, p := range m.Paths {
		if p.rgx.MatchString(r.URL.Path) {
			p.DNSFetch()

			number := p.rgx.ReplaceAllString(r.URL.Path, p.Number)
			r.URL.Path = p.rgx.ReplaceAllString(r.URL.Path, p.Replacement)

			target := &url.URL{
				Scheme: p.Schema,
				Host:   p.ipHolder.GetStr(number) + ":" + p.Port,
			}

			// slog.Debug("dnsPath replaced path", slog.String("path", r.URL.Path), slog.String("host", target.Host), slog.String("schema", target.Scheme))

			return target, p.client.HTTP.Transport
		}
	}

	return nil, nil
}

func (m *DNSPath) ForwardRequest(w http.ResponseWriter, r *http.Request, target *url.URL, transport http.RoundTripper) {
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = transport

	proxy.ServeHTTP(w, r)
}
