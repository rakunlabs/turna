package snirouter

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/rakunlabs/turna/pkg/server/snimatch"
	"github.com/rakunlabs/turna/pkg/server/tcp/tcpchain"
	"github.com/rakunlabs/turna/pkg/server/tcp/tcpmw"
)

// SNIRouter reads the server name of a TLS ClientHello without consuming it
// and runs the middlewares of the matching route. The TLS stream is passed
// unchanged, so routes can pass it through (redirect, load_balancer) or
// terminate it (tls_terminate).
type SNIRouter struct {
	Routes []Route `cfg:"routes"`
	// Default middlewares when no route matches, also used for non-TLS and
	// SNI-less connections. When empty such connections are rejected.
	Default []string `cfg:"default"`
	// Timeout to read the ClientHello, default is 5s.
	Timeout time.Duration `cfg:"timeout"`
}

type Route struct {
	// SNI names, "*.example.com" matches any subdomain.
	SNI         []string `cfg:"sni"`
	Middlewares []string `cfg:"middlewares"`
}

type route struct {
	names   []string
	handler tcpmw.Handler
}

func (m *SNIRouter) Middleware(_ context.Context, _ string) (tcpmw.Middleware, error) {
	if len(m.Routes) == 0 {
		return nil, errors.New("routes are required")
	}

	routes := make([]route, 0, len(m.Routes))

	for i, r := range m.Routes {
		if len(r.SNI) == 0 || len(r.Middlewares) == 0 {
			return nil, fmt.Errorf("route %d needs sni and middlewares", i)
		}

		names := make([]string, 0, len(r.SNI))
		for _, n := range r.SNI {
			names = append(names, strings.ToLower(strings.TrimSuffix(n, ".")))
		}

		routes = append(routes, route{names: names, handler: tcpchain.Lazy(r.Middlewares)})
	}

	var def tcpmw.Handler
	if len(m.Default) > 0 {
		def = tcpchain.Lazy(m.Default)
	}

	timeout := m.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	return func(_ tcpmw.Handler) tcpmw.Handler {
		return func(conn net.Conn) error {
			_ = conn.SetReadDeadline(time.Now().Add(timeout))

			c, br := tcpmw.BufferedConn(conn, recordHeaderLen+maxRecordLen)
			sni, _ := PeekSNI(br)

			_ = conn.SetReadDeadline(time.Time{})

			if h := match(routes, sni); h != nil {
				return h(c)
			}

			if def != nil {
				return def(c)
			}

			return fmt.Errorf("%w: no route for sni %q", tcpmw.ErrReject, sni)
		}
	}, nil
}

func match(routes []route, sni string) tcpmw.Handler {
	if sni == "" {
		return nil
	}

	sni = strings.ToLower(sni)

	for _, r := range routes {
		for _, n := range r.names {
			if snimatch.Match(n, sni) {
				return r.handler
			}
		}
	}

	return nil
}

const (
	recordHeaderLen = 5
	maxRecordLen    = 16384 + 2048
	recordTypeHS    = 0x16
)

type peeker interface {
	Peek(n int) ([]byte, error)
}

var errHelloParsed = errors.New("client hello parsed")

// PeekSNI returns the server name of a TLS ClientHello without consuming it.
func PeekSNI(r peeker) (string, error) {
	hdr, err := r.Peek(recordHeaderLen)
	if err != nil {
		return "", err
	}

	if hdr[0] != recordTypeHS {
		return "", errors.New("not a tls handshake")
	}

	length := int(binary.BigEndian.Uint16(hdr[3:5]))
	if length > maxRecordLen {
		return "", errors.New("tls record too large")
	}

	record, err := r.Peek(recordHeaderLen + length)
	if err != nil {
		return "", err
	}

	var sni string

	// let crypto/tls parse the hello from a copy of the peeked bytes
	err = tls.Server(&readOnlyConn{r: bytes.NewReader(record)}, &tls.Config{
		GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
			sni = hello.ServerName

			return nil, errHelloParsed
		},
	}).Handshake()
	if sni == "" && !errors.Is(err, errHelloParsed) {
		return "", err
	}

	return sni, nil
}

type readOnlyConn struct {
	net.Conn

	r io.Reader
}

func (c *readOnlyConn) Read(b []byte) (int, error)         { return c.r.Read(b) }
func (c *readOnlyConn) Write(b []byte) (int, error)        { return len(b), nil }
func (c *readOnlyConn) Close() error                       { return nil }
func (c *readOnlyConn) LocalAddr() net.Addr                { return nil }
func (c *readOnlyConn) RemoteAddr() net.Addr               { return nil }
func (c *readOnlyConn) SetDeadline(_ time.Time) error      { return nil }
func (c *readOnlyConn) SetReadDeadline(_ time.Time) error  { return nil }
func (c *readOnlyConn) SetWriteDeadline(_ time.Time) error { return nil }
