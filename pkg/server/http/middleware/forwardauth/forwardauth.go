// Package forwardauth asks an external service whether a request may pass,
// like Traefik ForwardAuth and nginx auth_request.
package forwardauth

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/internal/replay"
)

// hopHeaders are not sent to the auth service and not copied back.
var hopHeaders = []string{
	"Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization",
	"Te", "Trailer", "Transfer-Encoding", "Upgrade", "Content-Length", "Expect",
}

// forwardingHeaders describe the original request to auth services. Values
// sent by the client are removed unless TrustForwardHeader is set, so they
// cannot fake the request.
var forwardingHeaders = []string{"Forwarded", "X-Real-Ip"}

// forwardingPrefixes cover X-Forwarded-Port, X-Forwarded-Prefix and the
// X-Original-Url and X-Original-Method of nginx style auth services.
var forwardingPrefixes = []string{"X-Forwarded-", "X-Original-"}

// maxDrain is read from an allowing auth response so its connection can be
// reused; longer bodies close the connection instead.
const maxDrain = 64 << 10

type ForwardAuth struct {
	// Address of the auth service, e.g. http://authelia:9091/api/verify.
	Address string `cfg:"address"`
	// Method of the auth request, default GET. Use the original method with
	// "-".
	Method string `cfg:"method"`
	// Timeout of the auth request, default 10s.
	Timeout time.Duration `cfg:"timeout"`

	// TrustForwardHeader keeps X-Forwarded-* headers sent by the client,
	// otherwise they are replaced. Enable only behind a trusted proxy.
	TrustForwardHeader bool `cfg:"trust_forward_header"`
	// AuthRequestHeaders are the request headers sent to the auth service;
	// default is all of them.
	AuthRequestHeaders []string `cfg:"auth_request_headers"`
	// AuthResponseHeaders are copied from the auth response to the request
	// sent upstream, e.g. X-User. Request headers with the same names are
	// removed first so clients cannot set them.
	AuthResponseHeaders []string `cfg:"auth_response_headers"`
	// AuthResponseHeadersPrefix copies every auth response header starting
	// with it, e.g. X-Auth-.
	AuthResponseHeadersPrefix string `cfg:"auth_response_headers_prefix"`
	// AddAuthCookiesToResponse are cookies of the auth response set on the
	// upstream response, e.g. a refreshed session.
	AddAuthCookiesToResponse []string `cfg:"add_auth_cookies_to_response"`

	// ForwardBody sends the request body to the auth service.
	ForwardBody bool `cfg:"forward_body"`
	// MaxBodySize is the largest forwarded body, default 1MB. Larger
	// requests are rejected with 413.
	MaxBodySize int64 `cfg:"max_body_size"`

	// TLS of the auth service.
	TLS *TLS `cfg:"tls"`
}

type TLS struct {
	// CA is a PEM bundle of trusted CA certificates; empty uses the system roots.
	CA string `cfg:"ca"`
	// Cert and Key are a PEM client certificate and key.
	Cert string `cfg:"cert" log:"-"`
	Key  string `cfg:"key"  log:"-"`
	// InsecureSkipVerify disables verification of the certificate.
	InsecureSkipVerify bool `cfg:"insecure_skip_verify"`
}

func (t *TLS) config() (*tls.Config, error) {
	c := &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: t.InsecureSkipVerify} //nolint:gosec // opt-in

	if t.CA != "" {
		c.RootCAs = x509.NewCertPool()
		if !c.RootCAs.AppendCertsFromPEM([]byte(t.CA)) {
			return nil, errors.New("forward_auth tls: ca contains no certificates")
		}
	}

	if t.Cert != "" || t.Key != "" {
		cert, err := tls.X509KeyPair([]byte(t.Cert), []byte(t.Key))
		if err != nil {
			return nil, fmt.Errorf("forward_auth tls: %w", err)
		}

		c.Certificates = []tls.Certificate{cert}
	}

	return c, nil
}

func (m *ForwardAuth) Middleware(ctx context.Context) (func(http.Handler) http.Handler, error) {
	address, err := url.Parse(m.Address)
	if err != nil || (address.Scheme != "http" && address.Scheme != "https") || address.Host == "" {
		return nil, fmt.Errorf("forward_auth address %q must be an http or https URL", m.Address)
	}

	method := strings.ToUpper(strings.TrimSpace(m.Method))
	if method == "" {
		method = http.MethodGet
	}

	timeout := m.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	if timeout < 0 {
		return nil, errors.New("forward_auth timeout must not be negative")
	}

	maxBody := m.MaxBodySize
	if maxBody <= 0 {
		maxBody = 1 << 20
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	if m.TLS != nil {
		if transport.TLSClientConfig, err = m.TLS.config(); err != nil {
			return nil, err
		}
	}

	go func() {
		// a rebuilt middleware gets its own transport
		<-ctx.Done()
		transport.CloseIdleConnections()
	}()

	client := &http.Client{
		Transport: transport,
		Timeout:   timeout,
		// redirects of the auth service, e.g. to a login page, go to the client
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	canon := func(names []string) []string {
		out := make([]string, 0, len(names))
		for _, n := range names {
			if n = strings.TrimSpace(n); n != "" {
				out = append(out, http.CanonicalHeaderKey(n))
			}
		}

		return out
	}

	f := &forwarder{
		ForwardAuth:     m,
		address:         address.String(),
		method:          method,
		client:          client,
		maxBody:         maxBody,
		requestHeaders:  canon(m.AuthRequestHeaders),
		responseHeaders: canon(m.AuthResponseHeaders),
		responsePrefix:  http.CanonicalHeaderKey(strings.TrimSpace(m.AuthResponseHeadersPrefix)),
	}

	return f.middleware, nil
}

type forwarder struct {
	*ForwardAuth

	address         string
	method          string
	client          *http.Client
	maxBody         int64
	requestHeaders  []string
	responseHeaders []string
	responsePrefix  string
}

func (f *forwarder) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body []byte

		if f.ForwardBody && r.Body != nil && r.Body != http.NoBody {
			// read regardless of Content-Length, chunked uploads have none
			b, err := io.ReadAll(io.LimitReader(r.Body, f.maxBody+1))
			if err != nil {
				httputil.HandleError(w, httputil.NewError("cannot read body", err, http.StatusBadRequest))

				return
			}

			if int64(len(b)) > f.maxBody {
				httputil.HandleError(w, httputil.NewError("request body is too large", nil, http.StatusRequestEntityTooLarge))

				return
			}

			_ = r.Body.Close()
			body = b
			replay.SetBody(r, body)
			r.ContentLength = int64(len(body))
		}

		resp, err := f.check(r.Context(), r, body)
		if err != nil {
			if r.Context().Err() != nil {
				// the client went away
				slog.Debug("forward_auth request canceled", "address", f.address, "error", err)

				return
			}

			slog.Error("forward_auth request failed", "address", f.address, "error", err)
			httputil.HandleError(w, httputil.NewError("authentication service unavailable", nil, http.StatusBadGateway))

			return
		}

		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			f.deny(w, resp)
			_ = resp.Body.Close()

			return
		}

		// release the auth connection before the upstream runs, which may
		// take hours for websockets
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxDrain))
		_ = resp.Body.Close()

		f.copyAuthHeaders(r, resp)

		cookies := f.authCookies(resp)
		if len(cookies) == 0 {
			next.ServeHTTP(w, r)

			return
		}

		cw := &cookieWriter{ResponseWriter: w, cookies: cookies}
		next.ServeHTTP(cw, r)
		// an upstream that wrote nothing still answers 200 with the cookies
		cw.add()
	})
}

func (f *forwarder) check(ctx context.Context, r *http.Request, body []byte) (*http.Response, error) {
	method := f.method
	if method == "-" {
		method = r.Method
	}

	var reqBody io.Reader
	if body != nil {
		reqBody = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, f.address, reqBody)
	if err != nil {
		return nil, err
	}

	if len(f.requestHeaders) == 0 {
		for k, v := range r.Header {
			req.Header[k] = slices.Clone(v)
		}
	} else {
		for _, k := range f.requestHeaders {
			if v, ok := r.Header[k]; ok {
				req.Header[k] = slices.Clone(v)
			}
		}
	}

	removeHopHeaders(req.Header)

	if !f.TrustForwardHeader {
		removeForwarding(req.Header)
	}

	f.setForwarded(req.Header, r)

	return f.client.Do(req)
}

// removeHopHeaders removes hop-by-hop headers, also the ones listed in
// Connection.
func removeHopHeaders(h http.Header) {
	for _, v := range h.Values("Connection") {
		for name := range strings.SplitSeq(v, ",") {
			if name = strings.TrimSpace(name); name != "" {
				h.Del(name)
			}
		}
	}

	for _, k := range hopHeaders {
		h.Del(k)
	}
}

func removeForwarding(h http.Header) {
	for _, k := range forwardingHeaders {
		h.Del(k)
	}

	for k := range h {
		for _, p := range forwardingPrefixes {
			if strings.HasPrefix(k, p) {
				delete(h, k)
			}
		}
	}
}

func (f *forwarder) setForwarded(h http.Header, r *http.Request) {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}

	set := func(k, v string) {
		if f.TrustForwardHeader && h.Get(k) != "" {
			return
		}

		h.Set(k, v)
	}

	if f.TrustForwardHeader {
		if prior := h.Get("X-Forwarded-For"); prior != "" {
			ip = prior + ", " + ip
		}
	}

	proto := "http"
	if r.TLS != nil {
		proto = "https"
	}

	if f.TrustForwardHeader {
		// the proxy in front knows the scheme the client used
		proto = httputil.Scheme(r)
	}

	h.Set("X-Forwarded-For", ip)
	set("X-Forwarded-Method", r.Method)
	set("X-Forwarded-Proto", proto)
	set("X-Forwarded-Host", r.Host)
	set("X-Forwarded-Uri", r.URL.RequestURI())
}

// deny answers with the response of the auth service, e.g. 401 or a redirect
// to a login page.
func (f *forwarder) deny(w http.ResponseWriter, resp *http.Response) {
	src := resp.Header.Clone()
	removeHopHeaders(src)

	dst := w.Header()
	for k, v := range src {
		dst[k] = v
	}

	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, io.LimitReader(resp.Body, f.maxBody))
}

func (f *forwarder) copyAuthHeaders(r *http.Request, resp *http.Response) {
	for _, k := range f.responseHeaders {
		r.Header.Del(k)

		if v, ok := resp.Header[k]; ok {
			r.Header[k] = v
		}
	}

	if f.responsePrefix == "" {
		return
	}

	for k := range r.Header {
		if strings.HasPrefix(k, f.responsePrefix) {
			r.Header.Del(k)
		}
	}

	for k, v := range resp.Header {
		if strings.HasPrefix(k, f.responsePrefix) {
			r.Header[k] = v
		}
	}
}

func (f *forwarder) authCookies(resp *http.Response) []string {
	if len(f.AddAuthCookiesToResponse) == 0 {
		return nil
	}

	var out []string
	for _, c := range resp.Cookies() {
		if slices.Contains(f.AddAuthCookiesToResponse, c.Name) {
			out = append(out, c.String())
		}
	}

	return out
}

// cookieWriter adds cookies of the auth service to the upstream response.
type cookieWriter struct {
	http.ResponseWriter

	cookies []string
	written bool
}

func (w *cookieWriter) add() {
	if w.written {
		return
	}

	w.written = true
	for _, c := range w.cookies {
		w.ResponseWriter.Header().Add("Set-Cookie", c)
	}
}

func (w *cookieWriter) WriteHeader(code int) {
	if !replay.Informational(code) {
		w.add()
	}

	w.ResponseWriter.WriteHeader(code)
}

func (w *cookieWriter) Write(b []byte) (int, error) {
	w.add()

	return w.ResponseWriter.Write(b)
}

func (w *cookieWriter) Flush() {
	w.add()
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *cookieWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	w.written = true

	return http.NewResponseController(w.ResponseWriter).Hijack()
}

func (w *cookieWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
