package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
)

// Mirror sends a copy of requests to shadow upstreams. Mirror responses are
// discarded and never affect the client response.
type Mirror struct {
	Servers []MirrorServer `cfg:"servers"`
	// H2C uses HTTP/2 prior knowledge for plaintext mirror targets.
	H2C bool `cfg:"h2c"`
	// MaxBodySize is the maximum request body size in bytes to mirror,
	// requests with larger bodies are not mirrored. Default is 1MB.
	MaxBodySize int64 `cfg:"max_body_size"`
	// Timeout of a mirrored request, default is 10s.
	Timeout time.Duration `cfg:"timeout"`
	// MaxInFlight bounds concurrent mirrored requests, extra ones are dropped. Default is 100.
	MaxInFlight int `cfg:"max_in_flight"`
}

type MirrorServer struct {
	URL string `cfg:"url"`
	// Endpoints are alternative URLs for one mirror target. Exactly one is
	// selected for a mirrored request; URL and Endpoints are mutually exclusive.
	Endpoints []string `cfg:"endpoints"`
	// Percent of requests to mirror, 0-100. Default is 100.
	Percent *float64 `cfg:"percent"`
}

type mirrorTarget struct {
	urls    []*url.URL
	percent float64
}

type mirror struct {
	targets     []mirrorTarget
	maxBodySize int64
	timeout     time.Duration
	sem         chan struct{}
	client      *http.Client
}

func (m *Mirror) build(transport http.RoundTripper) (*mirror, error) {
	if m == nil || len(m.Servers) == 0 {
		return nil, nil
	}

	mr := &mirror{
		maxBodySize: m.MaxBodySize,
		timeout:     m.Timeout,
	}

	if mr.maxBodySize <= 0 {
		mr.maxBodySize = 1 << 20
	}

	if mr.timeout <= 0 {
		mr.timeout = 10 * time.Second
	}

	maxInFlight := m.MaxInFlight
	if maxInFlight <= 0 {
		maxInFlight = 100
	}

	mr.sem = make(chan struct{}, maxInFlight)
	mr.client = &http.Client{
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	for _, s := range m.Servers {
		if s.URL != "" && len(s.Endpoints) > 0 {
			return nil, fmt.Errorf("mirror URL and endpoints are mutually exclusive")
		}
		endpoints := s.Endpoints
		if len(endpoints) == 0 {
			endpoints = []string{s.URL}
		}
		var urls []*url.URL
		for _, endpoint := range endpoints {
			u, err := url.Parse(endpoint)
			if err != nil {
				return nil, fmt.Errorf("cannot parse mirror url %s: %w", endpoint, err)
			}
			if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				return nil, fmt.Errorf("mirror url must be an absolute HTTP(S) URL: %s", endpoint)
			}
			urls = append(urls, u)
		}

		percent := 100.0
		if s.Percent != nil {
			percent = *s.Percent
		}

		if percent < 0 || percent > 100 {
			return nil, fmt.Errorf("mirror percent must be between 0 and 100: %v", percent)
		}

		mr.targets = append(mr.targets, mirrorTarget{urls: urls, percent: percent})
	}

	return mr, nil
}

func (m *mirror) selectTargets() []*url.URL {
	var selected []*url.URL
	for _, t := range m.targets {
		if t.percent >= 100 || rand.Float64()*100 < t.percent {
			selected = append(selected, t.urls[rand.IntN(len(t.urls))])
		}
	}
	return selected
}

func (m *mirror) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if httputil.IsWebSocket(r) {
			next.ServeHTTP(w, r)

			return
		}

		selected := m.selectTargets()

		if len(selected) == 0 {
			next.ServeHTTP(w, r)

			return
		}

		body, ok := m.readBody(r)
		if !ok {
			next.ServeHTTP(w, r)

			return
		}

		// copy before the proxy modifies the request
		header := r.Header.Clone()
		reqURL := *r.URL
		method := r.Method
		host := r.Host

		next.ServeHTTP(w, r)

		for _, u := range selected {
			select {
			case m.sem <- struct{}{}:
			default:
				slog.Debug("mirror request dropped, too many in flight", "target", u.String())

				continue
			}

			go func(u *url.URL) {
				defer func() { <-m.sem }()

				m.send(u, method, host, &reqURL, header, body)
			}(u)
		}
	})
}

// readBody buffers the request body and restores it for the next handler.
func (m *mirror) readBody(r *http.Request) ([]byte, bool) {
	if r.Body == nil || r.Body == http.NoBody {
		return nil, true
	}

	if r.ContentLength > m.maxBodySize {
		return nil, false
	}

	buf, err := io.ReadAll(io.LimitReader(r.Body, m.maxBodySize+1))
	if err != nil {
		r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(buf), r.Body))

		return nil, false
	}

	if int64(len(buf)) > m.maxBodySize {
		// too large, give the proxy the full body back without mirroring
		r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(buf), r.Body))

		return nil, false
	}

	r.Body = io.NopCloser(bytes.NewReader(buf))

	return buf, true
}

func (m *mirror) send(target *url.URL, method, host string, reqURL *url.URL, header http.Header, body []byte) {
	ctx, cancel := context.WithTimeout(context.Background(), m.timeout)
	defer cancel()

	u := *target
	u.Path, u.RawPath = joinURLPath(target, reqURL)

	switch {
	case target.RawQuery == "" || reqURL.RawQuery == "":
		u.RawQuery = target.RawQuery + reqURL.RawQuery
	default:
		u.RawQuery = target.RawQuery + "&" + reqURL.RawQuery
	}

	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, u.String(), bodyReader)
	if err != nil {
		slog.Debug("mirror request cannot create", "target", target.String(), "err", err.Error())

		return
	}

	req.Header = header.Clone()
	for _, h := range []string{"Connection", "Keep-Alive", "Proxy-Connection", "Te", "Trailer", "Transfer-Encoding", "Upgrade"} {
		req.Header.Del(h)
	}

	req.Host = host

	resp, err := m.client.Do(req)
	if err != nil {
		slog.Debug("mirror request failed", "target", target.String(), "err", err.Error())

		return
	}

	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

// joinURLPath is the same path joining as httputil.NewSingleHostReverseProxy.
func joinURLPath(a, b *url.URL) (path, rawpath string) {
	if a.RawPath == "" && b.RawPath == "" {
		return singleJoiningSlash(a.Path, b.Path), ""
	}

	apath := a.EscapedPath()
	bpath := b.EscapedPath()

	aslash := strings.HasSuffix(apath, "/")
	bslash := strings.HasPrefix(bpath, "/")

	switch {
	case aslash && bslash:
		return a.Path + b.Path[1:], apath + bpath[1:]
	case !aslash && !bslash:
		return a.Path + "/" + b.Path, apath + "/" + bpath
	}

	return a.Path + b.Path, apath + bpath
}

func singleJoiningSlash(a, b string) string {
	aslash := strings.HasSuffix(a, "/")
	bslash := strings.HasPrefix(b, "/")

	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash:
		return a + "/" + b
	}

	return a + b
}

// MirrorMiddleware returns a middleware sending copies of requests to the
// mirror servers, independent of a proxy.
func (m *Mirror) MirrorMiddleware() (func(http.Handler) http.Handler, error) {
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("default transport is not *http.Transport")
	}

	transport = transport.Clone()
	if m != nil && m.H2C {
		var protocols http.Protocols
		protocols.SetUnencryptedHTTP2(true)
		protocols.SetHTTP2(true)
		transport.Protocols = &protocols
	}
	mr, err := m.build(transport)
	if err != nil {
		return nil, err
	}

	if mr == nil {
		return func(next http.Handler) http.Handler { return next }, nil
	}

	return mr.Middleware, nil
}
