package httpconnect

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/rakunlabs/turna/pkg/server/snimatch"
	"github.com/rakunlabs/turna/pkg/server/tcp/middleware/redirect"
	"github.com/rakunlabs/turna/pkg/server/tcp/tcpmw"
)

// HTTPConnect is an HTTP CONNECT (tunnel) proxy.
type HTTPConnect struct {
	// Users for Proxy-Authorization basic auth, user -> password.
	Users map[string]string `cfg:"users"`
	// AllowedHosts are host patterns ("example.com", "*.example.com") that
	// may be tunneled; empty allows all.
	AllowedHosts []string `cfg:"allowed_hosts"`
	// AllowedPorts limits target ports; empty allows all.
	AllowedPorts []int `cfg:"allowed_ports"`

	DialTimeout time.Duration `cfg:"dial_timeout"`
	// Timeout to read the CONNECT request, default is 10s.
	Timeout time.Duration `cfg:"timeout"`
}

func (m *HTTPConnect) Middleware(ctx context.Context, _ string) (tcpmw.Middleware, error) {
	dialTimeout := m.DialTimeout
	if dialTimeout <= 0 {
		dialTimeout = 10 * time.Second
	}

	timeout := m.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	f := &redirect.Forwarder{Network: "tcp", DialTimeout: dialTimeout}

	hosts := make([]string, 0, len(m.AllowedHosts))
	for _, h := range m.AllowedHosts {
		hosts = append(hosts, strings.ToLower(h))
	}

	return func(_ tcpmw.Handler) tcpmw.Handler {
		return func(conn net.Conn) error {
			_ = conn.SetReadDeadline(time.Now().Add(timeout))

			c, br := tcpmw.BufferedConn(conn, 4096)

			req, err := http.ReadRequest(br)
			if err != nil {
				return fmt.Errorf("%w: read connect request: %w", tcpmw.ErrReject, err)
			}

			_ = conn.SetReadDeadline(time.Time{})

			reply := func(code int, extra string) {
				fmt.Fprintf(conn, "HTTP/1.1 %d %s\r\n%sContent-Length: 0\r\nConnection: close\r\n\r\n", code, http.StatusText(code), extra)
			}

			if req.Method != http.MethodConnect {
				reply(http.StatusMethodNotAllowed, "Allow: CONNECT\r\n")

				return fmt.Errorf("%w: method %s not allowed", tcpmw.ErrReject, req.Method)
			}

			if len(m.Users) > 0 && !m.authorized(req.Header.Get("Proxy-Authorization")) {
				reply(http.StatusProxyAuthRequired, "Proxy-Authenticate: Basic realm=\"turna\"\r\n")

				return fmt.Errorf("%w: proxy authentication failed", tcpmw.ErrReject)
			}

			target := req.Host
			if target == "" {
				target = req.RequestURI
			}

			host, port, err := net.SplitHostPort(target)
			if err != nil {
				reply(http.StatusBadRequest, "")

				return fmt.Errorf("%w: invalid target %q", tcpmw.ErrReject, target)
			}

			if !m.allowed(hosts, strings.ToLower(host), port) {
				reply(http.StatusForbidden, "")

				return fmt.Errorf("%w: target %q not allowed", tcpmw.ErrReject, target)
			}

			rconn, err := f.Dial(ctx, c, target)
			if err != nil {
				reply(http.StatusBadGateway, "")

				return err
			}

			if _, err := fmt.Fprint(conn, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
				rconn.Close()

				return err
			}

			// data already buffered after the request belongs to the tunnel
			redirect.Forward(c, rconn)

			return nil
		}
	}, nil
}

func (m *HTTPConnect) authorized(header string) bool {
	raw, ok := strings.CutPrefix(header, "Basic ")
	if !ok {
		return false
	}

	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(raw))
	if err != nil {
		return false
	}

	user, pass, ok := strings.Cut(string(decoded), ":")
	if !ok {
		return false
	}

	want, ok := m.Users[user]

	return ok && subtle.ConstantTimeCompare([]byte(want), []byte(pass)) == 1
}

func (m *HTTPConnect) allowed(hosts []string, host, port string) bool {
	if len(m.AllowedPorts) > 0 {
		ok := false
		for _, p := range m.AllowedPorts {
			if fmt.Sprint(p) == port {
				ok = true

				break
			}
		}

		if !ok {
			return false
		}
	}

	if len(hosts) == 0 {
		return true
	}

	for _, h := range hosts {
		if snimatch.Match(h, host) {
			return true
		}
	}

	return false
}
