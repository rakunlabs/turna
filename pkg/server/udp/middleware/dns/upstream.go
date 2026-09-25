package dns

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/miekg/dns"
)

// upstream exchanges a DNS message with a resolver.
type upstream interface {
	Exchange(ctx context.Context, req *dns.Msg) (*dns.Msg, error)
	String() string
}

// newUpstream parses an upstream address:
//
//   - "1.1.1.1:53" or "udp://1.1.1.1:53": plain DNS over UDP, retried over
//     TCP when the answer is truncated,
//   - "tcp://1.1.1.1:53": plain DNS over TCP,
//   - "tls://1.1.1.1:853" or "tls://1.1.1.1:853#cloudflare-dns.com": DNS
//     over TLS, the name after # is used for certificate verification,
//   - "https://cloudflare-dns.com/dns-query": DNS over HTTPS (RFC 8484).
func newUpstream(raw string, timeout time.Duration, insecure bool) (upstream, error) {
	scheme, rest, ok := strings.Cut(raw, "://")
	if !ok {
		scheme, rest = "udp", raw
	}

	switch scheme {
	case "udp", "tcp":
		if _, _, err := net.SplitHostPort(rest); err != nil {
			return nil, fmt.Errorf("invalid upstream %q: %w", raw, err)
		}

		return &plainUpstream{
			addr: rest,
			udp:  &dns.Client{Net: scheme, Timeout: timeout},
			tcp:  &dns.Client{Net: "tcp", Timeout: timeout},
		}, nil
	case "tls":
		addr, serverName, _ := strings.Cut(rest, "#")

		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			host = addr
			addr = net.JoinHostPort(addr, "853")
		}

		if serverName == "" {
			serverName = host
		}

		return &plainUpstream{
			addr: addr,
			udp: &dns.Client{Net: "tcp-tls", Timeout: timeout, TLSConfig: &tls.Config{
				ServerName:         serverName,
				InsecureSkipVerify: insecure, //nolint:gosec // user option
				MinVersion:         tls.VersionTLS12,
			}},
			name: raw,
		}, nil
	case "https":
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" {
			return nil, fmt.Errorf("invalid upstream %q", raw)
		}

		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.TLSClientConfig = &tls.Config{
			InsecureSkipVerify: insecure, //nolint:gosec // user option
			MinVersion:         tls.VersionTLS12,
		}
		transport.ForceAttemptHTTP2 = true

		return &dohUpstream{
			url:    u.String(),
			client: &http.Client{Timeout: timeout, Transport: transport},
		}, nil
	}

	return nil, fmt.Errorf("unsupported upstream scheme %q in %q", scheme, raw)
}

type plainUpstream struct {
	addr string
	name string
	udp  *dns.Client
	// tcp retries truncated udp answers, nil when udp is not plain udp.
	tcp *dns.Client
}

func (u *plainUpstream) String() string {
	if u.name != "" {
		return u.name
	}

	return u.addr
}

func (u *plainUpstream) Exchange(ctx context.Context, req *dns.Msg) (*dns.Msg, error) {
	resp, _, err := u.udp.ExchangeContext(ctx, req, u.addr)
	if err != nil {
		return nil, err
	}

	if resp.Truncated && u.tcp != nil && u.udp.Net == "udp" {
		if full, _, err := u.tcp.ExchangeContext(ctx, req, u.addr); err == nil {
			return full, nil
		}
	}

	return resp, nil
}

type dohUpstream struct {
	url    string
	client *http.Client
}

func (u *dohUpstream) String() string { return u.url }

func (u *dohUpstream) Exchange(ctx context.Context, req *dns.Msg) (*dns.Msg, error) {
	// RFC 8484 recommends id 0 for cache friendliness
	q := req.Copy()
	q.Id = 0

	body, err := q.Pack()
	if err != nil {
		return nil, err
	}

	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, u.url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	hreq.Header.Set("Content-Type", "application/dns-message")
	hreq.Header.Set("Accept", "application/dns-message")

	hresp, err := u.client.Do(hreq)
	if err != nil {
		return nil, err
	}
	defer hresp.Body.Close()

	if hresp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("doh upstream %s returned %d", u.url, hresp.StatusCode)
	}

	raw, err := io.ReadAll(io.LimitReader(hresp.Body, 65535))
	if err != nil {
		return nil, err
	}

	resp := new(dns.Msg)
	if err := resp.Unpack(raw); err != nil {
		return nil, fmt.Errorf("unpack doh response: %w", err)
	}

	resp.Id = req.Id

	return resp, nil
}
