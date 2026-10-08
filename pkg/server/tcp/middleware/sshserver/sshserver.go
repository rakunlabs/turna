// Package sshserver implements an SSH server terminal TCP middleware.
package sshserver

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"

	"golang.org/x/crypto/ssh"

	"github.com/rakunlabs/turna/pkg/server/tcp/tcpmw"
)

type SSHServer struct {
	HostKeys []string `cfg:"host_keys" log:"-"`
	Banner   string   `cfg:"banner"`
	Users    []User   `cfg:"users"`
}

type User struct {
	Name               string      `cfg:"name"`
	AuthorizedKeys     []string    `cfg:"authorized_keys"`
	AuthorizedKeyFiles []string    `cfg:"authorized_key_files"`
	Shell              *Shell      `cfg:"shell"`
	SFTP               *SFTP       `cfg:"sftp"`
	Forwarding         *Forwarding `cfg:"forwarding"`

	keys []ssh.PublicKey
}

type Shell struct {
	Enabled   bool     `cfg:"enabled"`
	Command   string   `cfg:"command"`
	OSUser    string   `cfg:"os_user"`
	AllowExec bool     `cfg:"allow_exec"`
	Env       []string `cfg:"env" log:"-"`
}

// SFTP uses Turna's OS permissions. Directory is a working directory, not a
// filesystem jail; absolute paths remain available subject to those permissions.
type SFTP struct {
	Enabled   bool   `cfg:"enabled"`
	Directory string `cfg:"directory"`
	ReadOnly  bool   `cfg:"read_only"`
}

type Forwarding struct {
	Remote            bool     `cfg:"remote"`
	Local             bool     `cfg:"local"`
	AllowedRemoteBind []string `cfg:"allowed_remote_bind"`
	AllowedLocalDest  []string `cfg:"allowed_local_destination"`
	MaxRemoteForwards int      `cfg:"max_remote_forwards"`
	MaxConnections    int      `cfg:"max_connections"`
}

type server struct {
	config *ssh.ServerConfig
	users  map[string]*User
}

func (m *SSHServer) Middleware(ctx context.Context, name string) (tcpmw.Middleware, error) {
	s, err := m.build()
	if err != nil {
		return nil, err
	}

	return func(_ tcpmw.Handler) tcpmw.Handler {
		return func(conn net.Conn) error {
			return s.serve(ctx, name, conn)
		}
	}, nil
}

func (m *SSHServer) build() (*server, error) {
	if len(m.HostKeys) == 0 {
		return nil, errors.New("at least one host_key is required")
	}
	if len(m.Users) == 0 {
		return nil, errors.New("at least one user is required")
	}

	s := &server{users: make(map[string]*User)}
	s.config = &ssh.ServerConfig{BannerCallback: func(ssh.ConnMetadata) string { return m.Banner }}

	for _, path := range m.HostKeys {
		signer, err := readPrivateKey(path)
		if err != nil {
			return nil, fmt.Errorf("host key %q: %w", path, err)
		}
		s.config.AddHostKey(signer)
	}

	for i := range m.Users {
		u := &m.Users[i]
		if u.Name == "" {
			return nil, fmt.Errorf("users[%d]: name is required", i)
		}
		if _, exists := s.users[u.Name]; exists {
			return nil, fmt.Errorf("duplicate user %q", u.Name)
		}

		keys, err := loadAuthorizedKeys(u.AuthorizedKeys, u.AuthorizedKeyFiles)
		if err != nil {
			return nil, fmt.Errorf("user %q: %w", u.Name, err)
		}
		if len(keys) == 0 {
			return nil, fmt.Errorf("user %q: at least one authorized key is required", u.Name)
		}
		u.keys = keys
		if u.Forwarding != nil && (u.Forwarding.MaxRemoteForwards < 0 || u.Forwarding.MaxConnections < 0) {
			return nil, fmt.Errorf("user %q: forwarding limits cannot be negative", u.Name)
		}
		s.users[u.Name] = u
	}

	s.config.PublicKeyCallback = func(meta ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
		u := s.users[meta.User()]
		if u == nil {
			return nil, errors.New("public key authentication failed")
		}
		for _, allowed := range u.keys {
			if subtle.ConstantTimeCompare(allowed.Marshal(), key.Marshal()) == 1 {
				return &ssh.Permissions{Extensions: map[string]string{"turna-user": u.Name}}, nil
			}
		}
		return nil, errors.New("public key authentication failed")
	}

	return s, nil
}

func (s *server) serve(parent context.Context, middleware string, raw net.Conn) error {
	conn, channels, requests, err := ssh.NewServerConn(raw, s.config)
	if err != nil {
		return fmt.Errorf("ssh handshake: %w", err)
	}
	defer func() { _ = conn.Close() }()

	u := s.users[conn.User()]
	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	slog.Info("ssh connection opened", "middleware", middleware, "user", conn.User(), "remote", conn.RemoteAddr())
	defer slog.Info("ssh connection closed", "middleware", middleware, "user", conn.User(), "remote", conn.RemoteAddr())

	session := newConnectionSession(conn, u)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		session.handleGlobalRequests(requests)
	}()

	for newChannel := range channels {
		wg.Add(1)
		go func(ch ssh.NewChannel) {
			defer wg.Done()
			session.handleChannel(ctx, ch)
		}(newChannel)
	}

	cancel()
	session.close()
	wg.Wait()
	return nil
}
