package forward

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"

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
			if r.Method == http.MethodConnect {
				m.proxyConnect(w, r)

				return
			}

			proxy := httputil.NewSingleHostReverseProxy(r.URL)
			proxy.Transport = client.HTTP.Transport

			proxy.ServeHTTP(w, r)
		})
	}, nil
}

func (m *Forward) proxyConnect(w http.ResponseWriter, req *http.Request) error {
	slog.Debug(fmt.Sprintf("CONNECT requested to %v (from %v)", req.Host, req.RemoteAddr))
	targetConn, err := net.Dial("tcp", req.Host)
	if err != nil {
		slog.Error(fmt.Sprintf("failed to dial to target %v", req.Host), "err", err.Error())
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return nil
	}

	w.WriteHeader(http.StatusOK)
	hj, ok := w.(http.Hijacker)
	if !ok {
		return fmt.Errorf("http server doesn't support hijacking connection")
	}

	clientConn, _, err := hj.Hijack()
	if err != nil {
		return fmt.Errorf("http hijacking failed: %w", err)
	}

	slog.Debug(fmt.Sprintf("CONNECT tunnel established to %v (from %v)", req.Host, req.RemoteAddr))
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
