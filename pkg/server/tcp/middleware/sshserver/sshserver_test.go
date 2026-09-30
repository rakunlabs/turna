package sshserver

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

func testServer(t *testing.T, user User) (*ssh.Client, func()) {
	t.Helper()

	_, hostPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	hostDER, err := x509.MarshalPKCS8PrivateKey(hostPrivate)
	if err != nil {
		t.Fatal(err)
	}
	hostKeyPath := filepath.Join(t.TempDir(), "host_key")
	if err := os.WriteFile(hostKeyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: hostDER}), 0o600); err != nil {
		t.Fatal(err)
	}

	clientPublic, clientPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	clientSigner, err := ssh.NewSignerFromKey(clientPrivate)
	if err != nil {
		t.Fatal(err)
	}
	publicKey, err := ssh.NewPublicKey(clientPublic)
	if err != nil {
		t.Fatal(err)
	}
	user.AuthorizedKeys = []string{string(ssh.MarshalAuthorizedKey(publicKey))}

	middleware, err := (&SSHServer{HostKeys: []string{hostKeyPath}, Users: []User{user}}).Middleware(t.Context(), "test")
	if err != nil {
		t.Fatal(err)
	}
	handler := middleware(func(net.Conn) error { return nil })
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = conn.Close() }()
				_ = handler(conn)
			}()
		}
	}()

	client, err := ssh.Dial("tcp", listener.Addr().String(), &ssh.ClientConfig{
		User:            user.Name,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(clientSigner)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec // generated test key
		Timeout:         3 * time.Second,
	})
	if err != nil {
		_ = listener.Close()
		t.Fatal(err)
	}

	return client, func() {
		_ = client.Close()
		_ = listener.Close()
	}
}

func TestExec(t *testing.T) {
	client, closeServer := testServer(t, User{
		Name:  "test",
		Shell: &Shell{Enabled: true, Command: "/bin/sh", AllowExec: true},
	})
	defer closeServer()

	session, err := client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	output, err := session.CombinedOutput("printf turna")
	if err != nil {
		t.Fatal(err)
	}
	if string(output) != "turna" {
		t.Fatalf("output = %q", output)
	}
}

func TestPTYExec(t *testing.T) {
	client, closeServer := testServer(t, User{
		Name:  "test",
		Shell: &Shell{Enabled: true, Command: "/bin/sh", AllowExec: true},
	})
	defer closeServer()

	session, err := client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	if err := session.RequestPty("xterm", 24, 80, ssh.TerminalModes{}); err != nil {
		t.Fatal(err)
	}
	output, err := session.CombinedOutput("printf $TERM")
	if err != nil {
		t.Fatal(err)
	}
	if string(output) != "xterm" {
		t.Fatalf("output = %q", output)
	}
}

func TestRemoteForward(t *testing.T) {
	client, closeServer := testServer(t, User{
		Name: "test",
		Forwarding: &Forwarding{
			Remote: true, AllowedRemoteBind: []string{"127.0.0.1:*"},
		},
	})
	defer closeServer()

	forward, err := client.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = forward.Close() }()

	done := make(chan error, 1)
	go func() {
		conn, err := forward.Accept()
		if err != nil {
			done <- err
			return
		}
		defer func() { _ = conn.Close() }()
		_, err = io.Copy(conn, conn)
		done <- err
	}()

	conn, err := net.DialTimeout("tcp", forward.Addr().String(), 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := fmt.Fprint(conn, "hello\n"); err != nil {
		t.Fatal(err)
	}
	line := make([]byte, 6)
	if _, err := io.ReadFull(conn, line); err != nil {
		t.Fatal(err)
	}
	if string(line) != "hello\n" {
		t.Fatalf("response = %q", line)
	}
	_ = conn.Close()
	select {
	case err := <-done:
		if err != nil && !strings.Contains(err.Error(), "closed") {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("forward did not close")
	}
}

func TestLocalForward(t *testing.T) {
	echo, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = echo.Close() }()
	go func() {
		conn, err := echo.Accept()
		if err == nil {
			defer func() { _ = conn.Close() }()
			_, _ = io.Copy(conn, conn)
		}
	}()

	client, closeServer := testServer(t, User{
		Name: "test",
		Forwarding: &Forwarding{
			Local: true, AllowedLocalDest: []string{"127.0.0.1:*"},
		},
	})
	defer closeServer()

	conn, err := client.Dial("tcp", echo.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := fmt.Fprint(conn, "hello\n"); err != nil {
		t.Fatal(err)
	}
	line := make([]byte, 6)
	if _, err := io.ReadFull(conn, line); err != nil || string(line) != "hello\n" {
		t.Fatalf("response = %q, err = %v", line, err)
	}
}

func TestSFTP(t *testing.T) {
	directory := t.TempDir()
	client, closeServer := testServer(t, User{
		Name: "test",
		SFTP: &SFTP{Enabled: true, Directory: directory},
	})
	defer closeServer()

	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sftpClient.Close() }()

	f, err := sftpClient.Create("hello.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("turna")); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(directory, "hello.txt"))
	if err != nil || string(b) != "turna" {
		t.Fatalf("file = %q, err = %v", b, err)
	}
}

func TestConfigurationRequiresKeys(t *testing.T) {
	if _, err := (&SSHServer{}).Middleware(t.Context(), "test"); err == nil {
		t.Fatal("expected missing host key error")
	}
}
