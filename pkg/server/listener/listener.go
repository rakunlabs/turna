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
)

// Entrypoint is a bound socket of a configured entrypoint.
type Entrypoint struct {
	Name    string
	Address string
	Network string

	listener net.Listener
	packet   net.PacketConn

	mu      sync.Mutex
	current *Virtual
	closed  bool
	done    chan struct{}
}

// Bind opens the socket of an entrypoint.
func Bind(name, address, network string) (*Entrypoint, error) {
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
