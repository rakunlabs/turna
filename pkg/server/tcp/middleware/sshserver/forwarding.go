package sshserver

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"
)

type connectionSession struct {
	conn        *ssh.ServerConn
	user        *User
	mu          sync.Mutex
	listeners   map[string]net.Listener
	connections chan struct{}
}

type forwardRequest struct {
	Address string
	Port    uint32
}

type forwardedPayload struct {
	ConnectedAddress  string
	ConnectedPort     uint32
	OriginatorAddress string
	OriginatorPort    uint32
}

type directPayload struct {
	DestinationAddress string
	DestinationPort    uint32
	OriginatorAddress  string
	OriginatorPort     uint32
}

func newConnectionSession(conn *ssh.ServerConn, user *User) *connectionSession {
	maxConnections := 128
	if user.Forwarding != nil && user.Forwarding.MaxConnections > 0 {
		maxConnections = user.Forwarding.MaxConnections
	}
	return &connectionSession{
		conn: conn, user: user, listeners: make(map[string]net.Listener), connections: make(chan struct{}, maxConnections),
	}
}

func (s *connectionSession) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, listener := range s.listeners {
		_ = listener.Close()
		delete(s.listeners, key)
	}
}

func (s *connectionSession) handleGlobalRequests(requests <-chan *ssh.Request) {
	for req := range requests {
		switch req.Type {
		case "tcpip-forward":
			s.handleRemoteForward(req)
		case "cancel-tcpip-forward":
			s.handleCancelForward(req)
		default:
			if req.WantReply {
				_ = req.Reply(false, nil)
			}
		}
	}
}

func (s *connectionSession) handleRemoteForward(req *ssh.Request) {
	var payload forwardRequest
	if err := ssh.Unmarshal(req.Payload, &payload); err != nil || s.user.Forwarding == nil || !s.user.Forwarding.Remote {
		_ = req.Reply(false, nil)
		return
	}
	if !allowedAddress(payload.Address, payload.Port, s.user.Forwarding.AllowedRemoteBind, true) {
		slog.Warn("ssh remote forward denied", "user", s.user.Name, "address", payload.Address, "port", payload.Port)
		_ = req.Reply(false, nil)
		return
	}

	s.mu.Lock()
	maxForwards := s.user.Forwarding.MaxRemoteForwards
	if maxForwards == 0 {
		maxForwards = 8
	}
	if len(s.listeners) >= maxForwards {
		s.mu.Unlock()
		_ = req.Reply(false, nil)
		return
	}
	s.mu.Unlock()

	listener, err := net.Listen("tcp", net.JoinHostPort(payload.Address, strconv.FormatUint(uint64(payload.Port), 10)))
	if err != nil {
		_ = req.Reply(false, nil)
		return
	}
	actual := listener.Addr().(*net.TCPAddr)
	key := forwardKey(payload.Address, uint32(actual.Port))

	s.mu.Lock()
	if _, exists := s.listeners[key]; exists {
		s.mu.Unlock()
		_ = listener.Close()
		_ = req.Reply(false, nil)
		return
	}
	s.listeners[key] = listener
	s.mu.Unlock()

	response := []byte(nil)
	if payload.Port == 0 {
		response = ssh.Marshal(struct{ Port uint32 }{uint32(actual.Port)})
	}
	if err := req.Reply(true, response); err != nil {
		s.removeListener(key)
		return
	}

	slog.Info("ssh remote forward opened", "user", s.user.Name, "address", payload.Address, "port", actual.Port)
	go s.acceptRemoteForward(key, listener, payload.Address, uint32(actual.Port))
}

func (s *connectionSession) acceptRemoteForward(key string, listener net.Listener, bind string, port uint32) {
	defer s.removeListener(key)
	for {
		incoming, err := listener.Accept()
		if err != nil {
			return
		}
		if !s.acquireConnection() {
			_ = incoming.Close()
			continue
		}
		go func() {
			defer s.releaseConnection()
			defer func() { _ = incoming.Close() }()
			host, sourcePort := addrParts(incoming.RemoteAddr())
			channel, requests, err := s.conn.OpenChannel("forwarded-tcpip", ssh.Marshal(forwardedPayload{
				ConnectedAddress: bind, ConnectedPort: port, OriginatorAddress: host, OriginatorPort: sourcePort,
			}))
			if err != nil {
				return
			}
			defer func() { _ = channel.Close() }()
			go ssh.DiscardRequests(requests)
			pipeSSH(channel, incoming)
		}()
	}
}

func (s *connectionSession) handleCancelForward(req *ssh.Request) {
	var payload forwardRequest
	if err := ssh.Unmarshal(req.Payload, &payload); err != nil {
		_ = req.Reply(false, nil)
		return
	}
	key := forwardKey(payload.Address, payload.Port)
	s.mu.Lock()
	listener, ok := s.listeners[key]
	if ok {
		delete(s.listeners, key)
	}
	s.mu.Unlock()
	if ok {
		_ = listener.Close()
		slog.Info("ssh remote forward closed", "user", s.user.Name, "address", payload.Address, "port", payload.Port)
	}
	_ = req.Reply(ok, nil)
}

func (s *connectionSession) removeListener(key string) {
	s.mu.Lock()
	listener, ok := s.listeners[key]
	if ok {
		delete(s.listeners, key)
	}
	s.mu.Unlock()
	if ok {
		_ = listener.Close()
	}
}

func (s *connectionSession) handleDirectChannel(ctx context.Context, newChannel ssh.NewChannel) {
	if s.user.Forwarding == nil || !s.user.Forwarding.Local {
		_ = newChannel.Reject(ssh.Prohibited, "local forwarding is disabled")
		return
	}
	var payload directPayload
	if err := ssh.Unmarshal(newChannel.ExtraData(), &payload); err != nil ||
		!allowedAddress(payload.DestinationAddress, payload.DestinationPort, s.user.Forwarding.AllowedLocalDest, false) {
		_ = newChannel.Reject(ssh.Prohibited, "destination is not allowed")
		return
	}
	if !s.acquireConnection() {
		_ = newChannel.Reject(ssh.ResourceShortage, "forwarding connection limit reached")
		return
	}
	defer s.releaseConnection()

	target := net.JoinHostPort(payload.DestinationAddress, strconv.FormatUint(uint64(payload.DestinationPort), 10))
	upstream, err := (&net.Dialer{}).DialContext(ctx, "tcp", target)
	if err != nil {
		_ = newChannel.Reject(ssh.ConnectionFailed, err.Error())
		return
	}
	defer func() { _ = upstream.Close() }()

	channel, requests, err := newChannel.Accept()
	if err != nil {
		return
	}
	defer func() { _ = channel.Close() }()
	go ssh.DiscardRequests(requests)
	pipeSSH(channel, upstream)
}

func (s *connectionSession) acquireConnection() bool {
	select {
	case s.connections <- struct{}{}:
		return true
	default:
		return false
	}
}

func (s *connectionSession) releaseConnection() { <-s.connections }

func pipeSSH(channel ssh.Channel, conn net.Conn) {
	done := make(chan error, 2)
	go func() {
		_, err := io.Copy(conn, channel)
		if err == nil {
			if tcp, ok := conn.(*net.TCPConn); ok {
				err = tcp.CloseWrite()
			}
		}
		done <- err
	}()
	go func() {
		_, err := io.Copy(channel, conn)
		if err == nil {
			err = channel.CloseWrite()
		}
		done <- err
	}()
	for range 2 {
		if err := <-done; err != nil {
			_ = conn.Close()
			_ = channel.Close()
		}
	}
}

func (s *connectionSession) handleChannel(ctx context.Context, ch ssh.NewChannel) {
	switch ch.ChannelType() {
	case "session":
		s.handleSessionChannel(ctx, ch)
	case "direct-tcpip":
		s.handleDirectChannel(ctx, ch)
	default:
		_ = ch.Reject(ssh.UnknownChannelType, "unsupported channel type")
	}
}

func allowedAddress(host string, port uint32, rules []string, remote bool) bool {
	if len(rules) == 0 {
		if !remote {
			return false
		}
		return host == "localhost" || host == "127.0.0.1" || host == "::1"
	}
	for _, rule := range rules {
		ruleHost, rulePort, err := net.SplitHostPort(rule)
		if err != nil || ruleHost != "*" && !strings.EqualFold(ruleHost, host) {
			continue
		}
		if portAllowed(port, rulePort) {
			return true
		}
	}
	return false
}

func portAllowed(port uint32, rule string) bool {
	if rule == "*" {
		return true
	}
	if first, last, ok := strings.Cut(rule, "-"); ok {
		lo, err1 := strconv.ParseUint(first, 10, 16)
		hi, err2 := strconv.ParseUint(last, 10, 16)
		return err1 == nil && err2 == nil && uint64(port) >= lo && uint64(port) <= hi
	}
	want, err := strconv.ParseUint(rule, 10, 16)
	return err == nil && uint64(port) == want
}

func forwardKey(host string, port uint32) string { return fmt.Sprintf("%s:%d", host, port) }

func addrParts(addr net.Addr) (string, uint32) {
	host, port, err := net.SplitHostPort(addr.String())
	if err != nil {
		return addr.String(), 0
	}
	n, _ := strconv.ParseUint(port, 10, 16)
	return host, uint32(n)
}
