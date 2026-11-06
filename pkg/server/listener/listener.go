// Package listener manages entrypoint sockets. A TCP entrypoint keeps one
// real listener and hands accepted connections to the current consumer, so a
// consumer (HTTP or TCP server) can be replaced without closing the socket.
package listener

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"

	"github.com/quic-go/quic-go"
)

// Entrypoint is a bound socket of a configured entrypoint.
type Entrypoint struct {
	Name    string
	Address string
	Network string

	listener net.Listener
	packet   net.PacketConn
	// quic is the HTTP/3 transport on the UDP port of a TCP entrypoint
	quic *quic.Transport

	mu      sync.Mutex
	current *Virtual
	closed  bool
	done    chan struct{}
}

// Bind opens the socket of an entrypoint.
func Bind(name, address, network string) (*Entrypoint, error) {
	return BindWith(name, address, network, false)
}

// BindWith opens the socket of an entrypoint; quic also opens a UDP socket
// on the same port of a TCP entrypoint for HTTP/3.
func BindWith(name, address, network string, quic bool) (*Entrypoint, error) {
	if network == "" {
		network = "tcp"
	}

	e := &Entrypoint{Name: name, Address: address, Network: network, done: make(chan struct{})}

	if e.IsPacket() {
		conn, err := net.ListenPacket(network, address)
		if err != nil {
			return nil, fmt.Errorf("address cannot listen %s: %w", address, err)
		}

		e.packet = conn
		slog.Info(fmt.Sprintf("entrypoint %s is listening on %s with %s", name, conn.LocalAddr().String(), network))

		return e, nil
	}

	l, err := net.Listen(network, address)
	if err != nil {
		return nil, fmt.Errorf("address cannot listen %s: %w", address, err)
	}

	e.listener = l
	slog.Info(fmt.Sprintf("entrypoint %s is listening on %s with %s", name, l.Addr().String(), network))

	if quic {
		conn, err := e.OpenQUIC()
		if err != nil {
			_ = l.Close()

			return nil, err
		}

		e.quic = conn
	}

	go e.acceptLoop()

	return e, nil
}

// Same reports whether the entrypoint is bound with the given settings.
func (e *Entrypoint) Same(address, network string) bool {
	if network == "" {
		network = "tcp"
	}

	return e.Address == address && e.Network == network
}

// QUIC returns the HTTP/3 transport, nil when not enabled.
func (e *Entrypoint) QUIC() *quic.Transport {
	e.mu.Lock()
	defer e.mu.Unlock()

	return e.quic
}

// OpenQUIC opens a QUIC transport on the UDP port of the TCP entrypoint
// without attaching it, see SetQUIC.
func (e *Entrypoint) OpenQUIC() (*quic.Transport, error) {
	tcpAddr, ok := e.listener.Addr().(*net.TCPAddr)
	if !ok {
		return nil, fmt.Errorf("entrypoint %s: http3 needs a tcp network", e.Name)
	}

	udpNetwork := "udp" + strings.TrimPrefix(e.Network, "tcp")

	conn, err := net.ListenPacket(udpNetwork, net.JoinHostPort(addrHost(e.Address), fmt.Sprint(tcpAddr.Port)))
	if err != nil {
		return nil, fmt.Errorf("address cannot listen %s for http3: %w", e.Address, err)
	}

	slog.Info(fmt.Sprintf("entrypoint %s is listening on %s with %s for http3", e.Name, conn.LocalAddr().String(), udpNetwork))

	// the transport owns the socket and outlives the servers using it
	return &quic.Transport{Conn: conn}, nil
}

// SetQUIC replaces the HTTP/3 transport, closing the previous one and its
// socket; nil disables HTTP/3.
func (e *Entrypoint) SetQUIC(conn *quic.Transport) {
	e.mu.Lock()
	old := e.quic
	e.quic = conn
	e.mu.Unlock()

	if old != nil && old != conn {
		_ = old.Close()
		_ = old.Conn.Close()
	}
}

func addrHost(address string) string {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return ""
	}

	return host
}

// IsPacket reports whether the entrypoint is a UDP (packet) socket.
func (e *Entrypoint) IsPacket() bool {
	return strings.HasPrefix(e.Network, "udp")
}

// Addr returns the bound address.
func (e *Entrypoint) Addr() net.Addr {
	if e.packet != nil {
		return e.packet.LocalAddr()
	}

	return e.listener.Addr()
}

// PacketConn returns the UDP socket, nil for stream entrypoints.
func (e *Entrypoint) PacketConn() net.PacketConn {
	return e.packet
}

// Listen returns a new listener receiving the connections of the entrypoint.
// The previous listener returned by Listen is closed; closing a returned
// listener does not close the entrypoint socket.
func (e *Entrypoint) Listen() (net.Listener, error) {
	if e.listener == nil {
		return nil, fmt.Errorf("entrypoint %s is not a stream listener", e.Name)
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return nil, net.ErrClosed
	}

	if e.current != nil {
		e.current.closeOnce()
	}

	v := &Virtual{addr: e.listener.Addr(), conns: make(chan net.Conn), done: make(chan struct{})}
	e.current = v

	return v, nil
}

// Close closes the socket and the current listener.
func (e *Entrypoint) Close() error {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()

		return nil
	}

	e.closed = true
	if e.current != nil {
		e.current.closeOnce()
		e.current = nil
	}
	e.mu.Unlock()

	if e.packet != nil {
		return e.packet.Close()
	}

	err := e.listener.Close()
	<-e.done

	e.SetQUIC(nil)

	return err
}

func (e *Entrypoint) acceptLoop() {
	defer close(e.done)

	for {
		conn, err := e.listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}

			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				continue
			}

			slog.Warn("failed to accept connection", "entrypoint", e.Name, "err", err.Error())

			continue
		}

		e.mu.Lock()
		v := e.current
		e.mu.Unlock()

		if v == nil || !v.deliver(conn) {
			_ = conn.Close()
		}
	}
}

// Virtual is a listener fed by an Entrypoint.
type Virtual struct {
	addr  net.Addr
	conns chan net.Conn
	done  chan struct{}
	once  sync.Once
}

func (v *Virtual) deliver(conn net.Conn) bool {
	select {
	case v.conns <- conn:
		return true
	case <-v.done:
		return false
	}
}

func (v *Virtual) Accept() (net.Conn, error) {
	select {
	case conn := <-v.conns:
		return conn, nil
	case <-v.done:
		return nil, net.ErrClosed
	}
}

func (v *Virtual) Close() error {
	v.closeOnce()

	return nil
}

func (v *Virtual) closeOnce() {
	v.once.Do(func() { close(v.done) })
}

func (v *Virtual) Addr() net.Addr {
	return v.addr
}
