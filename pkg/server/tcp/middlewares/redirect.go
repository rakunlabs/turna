package middlewares

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

type Redirect struct {
	Address string `cfg:"address"`

	DisableNagle bool `cfg:"disable_nagle"`
	Buffer       int  `cfg:"buffer"`

	DialTimeout time.Duration `cfg:"dial_timeout"`

	ProxyProtocol bool `cfg:"proxy_protocol"`
}

func (m *Redirect) Middleware(ctx context.Context, name string) (func(lconn *net.TCPConn) error, error) {
	raddr, err := net.ResolveTCPAddr("tcp", m.Address)
	if err != nil {
		return nil, fmt.Errorf("address cannot resolve %s: %w", m.Address, err)
	}

	if m.Buffer <= 0 {
		m.Buffer = 0xffff
	}

	return func(lconn *net.TCPConn) error {
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		wg := &sync.WaitGroup{}

		var rconn *net.TCPConn
		if m.DialTimeout > 0 {
			dialCtx, cancel := context.WithTimeout(ctx, m.DialTimeout)
			defer cancel()

			d := net.Dialer{}
			conn, err := d.DialContext(dialCtx, "tcp", m.Address)
			if err != nil {
				return fmt.Errorf("failed to dial to %s: %w", m.Address, err)
			}

			rconn = conn.(*net.TCPConn)
		} else {
			var err error
			rconn, err = net.DialTCP("tcp", nil, raddr)
			if err != nil {
				return fmt.Errorf("failed to dial to %s: %w", m.Address, err)
			}
		}

		defer rconn.Close()

		if m.DisableNagle {
			_ = lconn.SetNoDelay(true)
			_ = rconn.SetNoDelay(true)
		}

		if m.ProxyProtocol {
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

		log.Debug().Msgf("connection from %s to %s opened", lconn.RemoteAddr(), rconn.RemoteAddr())

		// bidirectional copy
		wg.Add(2)
		go m.pipe(ctx, wg, cancel, lconn, rconn, true)
		go m.pipe(ctx, wg, cancel, rconn, lconn, false)

		// wait for error
		wg.Wait()

		_ = lconn.Close()
		_ = rconn.Close()

		log.Debug().Msgf("connection from %s to %s closed", lconn.RemoteAddr(), rconn.RemoteAddr())

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
			log.Warn().Err(ctx.Err()).Msg("context done")

			return
		default:
		}

		n, err := src.Read(buff)
		if err != nil {
			if !(errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed)) {
				log.Warn().Err(err).Msg("read failed")
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
				log.Warn().Err(err).Msg("write failed")
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
