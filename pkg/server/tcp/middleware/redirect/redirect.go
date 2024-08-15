package redirect

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"
)

type Redirect struct {
	Address string `cfg:"address"`
	Network string `cfg:"network"`

	DisableNagle bool `cfg:"disable_nagle"`
	Buffer       int  `cfg:"buffer"`

	DialTimeout time.Duration `cfg:"dial_timeout"`

	ProxyProtocol bool `cfg:"proxy_protocol"`
}

func (m *Redirect) Middleware(ctx context.Context, _ string) (func(lconn *net.TCPConn) error, error) {
	network := m.Network
	if network == "" {
		network = "tcp"
	}

	proxyProtocol := false

	switch network {
	case "tcp", "tcp4", "tcp6":
		if _, err := net.ResolveTCPAddr(network, m.Address); err != nil {
			return nil, fmt.Errorf("address cannot resolve %s: %w", m.Address, err)
		}
		proxyProtocol = m.ProxyProtocol
	case "unix", "unixpacket":
		if _, err := net.ResolveUnixAddr(network, m.Address); err != nil {
			return nil, fmt.Errorf("address cannot resolve %s: %w", m.Address, err)
		}
	case "udp", "udp4", "udp6":
		if _, err := net.ResolveUDPAddr(network, m.Address); err != nil {
			return nil, fmt.Errorf("address cannot resolve %s: %w", m.Address, err)
		}
	default:
		return nil, fmt.Errorf("unsupported network %s", network)
	}

	if m.Buffer <= 0 {
		m.Buffer = 0xffff
	}

	return func(lconn *net.TCPConn) error {
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		wg := &sync.WaitGroup{}

		var rconn net.Conn
		if m.DialTimeout > 0 {
			dialCtx, cancel := context.WithTimeout(ctx, m.DialTimeout)
			defer cancel()

			d := net.Dialer{}
			conn, err := d.DialContext(dialCtx, network, m.Address)
			if err != nil {
				return fmt.Errorf("failed to dial to %s: %w", m.Address, err)
			}

			rconn = conn
		} else {
			var err error
			rconn, err = net.Dial(network, m.Address)
			if err != nil {
				return fmt.Errorf("failed to dial to %s: %w", m.Address, err)
			}
		}

		defer rconn.Close()

		if m.DisableNagle {
			_ = lconn.SetNoDelay(true)
			if rconn, ok := rconn.(*net.TCPConn); ok {
				_ = rconn.SetNoDelay(true)
			}
		}

		if proxyProtocol {
			// Append protocol version
			proxyBuilder := strings.Builder{}
			proxyBuilder.WriteString("PROXY ")
			if lconn.RemoteAddr().(*net.TCPAddr).IP.To4() != nil {
				proxyBuilder.WriteString("TCP4 ")
			} else {
				proxyBuilder.WriteString("TCP6 ")
			}

			// Append source and destination IP
			proxyBuilder.WriteString(lconn.RemoteAddr().(*net.TCPAddr).IP.String())
			proxyBuilder.WriteString(" ")
			proxyBuilder.WriteString(rconn.RemoteAddr().(*net.TCPAddr).IP.String())
			proxyBuilder.WriteString(" ")

			// Append port
			proxyBuilder.WriteString(fmt.Sprintf("%d %d\r\n", lconn.RemoteAddr().(*net.TCPAddr).Port, rconn.RemoteAddr().(*net.TCPAddr).Port))

			if _, err := rconn.Write([]byte(proxyBuilder.String())); err != nil {
				return fmt.Errorf("failed to write proxy protocol: %w", err)
			}
		}

		slog.Debug(fmt.Sprintf("connection from %s to %s opened", lconn.RemoteAddr(), rconn.RemoteAddr()))

		// bidirectional copy
		wg.Add(2)
		go m.pipe(ctx, wg, cancel, lconn, rconn, true)
		go m.pipe(ctx, wg, cancel, rconn, lconn, false)

		// wait for error
		wg.Wait()

		_ = lconn.Close()
		_ = rconn.Close()

		slog.Debug(fmt.Sprintf("connection from %s to %s closed", lconn.RemoteAddr(), rconn.RemoteAddr()))

		return nil
	}, nil
}

func (m *Redirect) pipe(ctx context.Context, wg *sync.WaitGroup, cancel context.CancelFunc, src, dst io.ReadWriteCloser, closeWay bool) {
	defer wg.Done()

	// directional copy (64k buffer)
	buff := make([]byte, m.Buffer)
	for {
		select {
		case <-ctx.Done():
			slog.Warn("context done", "err", ctx.Err())

			return
		default:
		}

		n, err := src.Read(buff)
		if err != nil {
			if !(errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed)) {
				slog.Warn("read failed", "err", err.Error())
			}

			cancel()

			if closeWay {
				_ = dst.Close()
			} else {
				_ = src.Close()
			}

			return
		}

		b := buff[:n]

		_, err = dst.Write(b)
		if err != nil {
			if !(errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed)) {
				slog.Warn("write failed", "err", err.Error())
			}

			cancel()

			if closeWay {
				_ = dst.Close()
			} else {
				_ = src.Close()
			}

			return
		}
	}
}
