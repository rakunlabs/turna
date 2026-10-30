package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"
)

// unixSockets maps placeholder hosts to unix socket paths. Upstreams written
// as unix:///path/to.sock are proxied as http://<placeholder>, and the
// transport dials the socket for that host.
type unixSockets map[string]string

func isUnixURL(u *url.URL) bool {
	return u.Scheme == "unix"
}

// rewrite returns the http URL used for a unix socket upstream.
func (s unixSockets) rewrite(raw string, u *url.URL) (*url.URL, error) {
	socket := u.Path
	if socket == "" {
		socket = u.Opaque
	}

	if u.Host != "" || socket == "" || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("unix url %s must be unix:///path/to.sock", raw)
	}

	sum := sha256.Sum256([]byte(socket))
	host := "unix-" + hex.EncodeToString(sum[:6]) + ".sock"
	s[host] = socket

	return &url.URL{Scheme: "http", Host: host}, nil
}

// wrap makes the transport dial unix sockets for placeholder hosts and skips
// proxies for them.
func (s unixSockets) wrap(transport *http.Transport) {
	if len(s) == 0 {
		return
	}

	dial := transport.DialContext
	if dial == nil {
		dial = (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	}

	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if host, _, err := net.SplitHostPort(addr); err == nil {
			if socket, ok := s[host]; ok {
				return dial(ctx, "unix", socket)
			}
		}

		return dial(ctx, network, addr)
	}

	if proxy := transport.Proxy; proxy != nil {
		transport.Proxy = func(r *http.Request) (*url.URL, error) {
			if _, ok := s[r.URL.Hostname()]; ok {
				return nil, nil
			}

			return proxy(r)
		}
	}
}
