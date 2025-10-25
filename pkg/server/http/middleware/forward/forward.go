package forward

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/worldline-go/klient"
)

type Forward struct {
	InsecureSkipVerify bool `cfg:"insecure_skip_verify"`
}

func (m *Forward) Middleware() (func(http.Handler) http.Handler, error) {
	client, err := klient.NewPlain(
		klient.WithInsecureSkipVerify(m.InsecureSkipVerify),
	)
	if err != nil {
		return nil, fmt.Errorf("cannot create klient: %w", err)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			slog.Info("URL request", "path", r.URL.Path, "host", r.Host, "scheme", r.URL.Scheme, "method", r.Method)

			if r.Method == http.MethodConnect {
				err := m.proxyConnect(w, r)
				if err != nil {
					slog.Error("CONNECT proxy failed", "err", err.Error())
				}

				return
			}

			// For regular HTTP requests, create a proper proxy
			targetURL := &url.URL{
				Scheme: r.URL.Scheme,
				Host:   r.Host,
			}

			// If no scheme is specified, assume http for non-CONNECT requests
			if targetURL.Scheme == "" {
				targetURL.Scheme = "http"
			}

			proxy := httputil.NewSingleHostReverseProxy(targetURL)
			proxy.Transport = client.HTTP.Transport

			// Modify the request to include the full URL
			r.URL.Scheme = targetURL.Scheme
			r.URL.Host = targetURL.Host
			r.RequestURI = ""

			proxy.ServeHTTP(w, r)
		})
	}, nil
}

func (m *Forward) proxyConnect(w http.ResponseWriter, req *http.Request) error {
	slog.Debug(fmt.Sprintf("CONNECT requested to %v (from %v)", req.Host, req.RemoteAddr))

	// Ensure the host includes a port
	host := req.Host
	if !strings.Contains(host, ":") {
		host = net.JoinHostPort(host, "443") // Default HTTPS port
	}

	dialer := &net.Dialer{}

	targetConn, err := dialer.DialContext(req.Context(), "tcp", host)
	if err != nil {
		slog.Error(fmt.Sprintf("failed to dial to target %v", host), "err", err.Error())
		http.Error(w, err.Error(), http.StatusServiceUnavailable)

		return fmt.Errorf("failed to dial target: %w", err)
	}

	w.WriteHeader(http.StatusOK)

	hj, ok := w.(http.Hijacker)
	if !ok {
		targetConn.Close()

		return errors.New("http server doesn't support hijacking connection")
	}

	clientConn, _, err := hj.Hijack()
	if err != nil {
		targetConn.Close()

		return fmt.Errorf("http hijacking failed: %w", err)
	}

	slog.Debug(fmt.Sprintf("CONNECT tunnel established to %v (from %v)", host, req.RemoteAddr))

	go m.tunnelConn(targetConn, clientConn)
	go m.tunnelConn(clientConn, targetConn)

	return nil
}

func (m *Forward) tunnelConn(dst io.WriteCloser, src io.ReadCloser) {
	_, err := io.Copy(dst, src)
	if err != nil && !errors.Is(err, net.ErrClosed) {
		slog.Error("failed to copy data", "err", err.Error())
	}

	dst.Close()
	src.Close()
}
