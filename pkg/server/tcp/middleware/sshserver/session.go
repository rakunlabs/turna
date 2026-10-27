package sshserver

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/kballard/go-shellquote"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type ptyRequest struct {
	Term   string
	Cols   uint32
	Rows   uint32
	Width  uint32
	Height uint32
	Modes  string
}

type windowChange struct {
	Cols   uint32
	Rows   uint32
	Width  uint32
	Height uint32
}

type envRequest struct {
	Name  string
	Value string
}

type execRequest struct{ Command string }
type subsystemRequest struct{ Name string }

type terminal interface {
	io.ReadWriteCloser
	Resize(cols, rows uint32) error
}

func (s *connectionSession) handleSessionChannel(ctx context.Context, newChannel ssh.NewChannel) {
	channel, requests, err := newChannel.Accept()
	if err != nil {
		return
	}
	defer func() { _ = channel.Close() }()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		mu      sync.Mutex
		started bool
		ptyCfg  *ptyRequest
		term    terminal
		output  <-chan struct{}
		cmd     *exec.Cmd
		env     = make(map[string]string)
	)

	for req := range requests {
		switch req.Type {
		case "pty-req":
			var requested ptyRequest
			ok := s.user.Shell != nil && s.user.Shell.Enabled && ssh.Unmarshal(req.Payload, &requested) == nil
			mu.Lock()
			if started || ptyCfg != nil {
				ok = false
			} else if ok {
				ptyCfg = &requested
			}
			mu.Unlock()
			reply(req, ok)

		case "env":
			var requested envRequest
			ok := s.user.Shell != nil && ssh.Unmarshal(req.Payload, &requested) == nil &&
				slices.Contains(s.user.Shell.Env, requested.Name) && !strings.ContainsAny(requested.Name, "=\x00") &&
				!strings.ContainsRune(requested.Value, '\x00')
			mu.Lock()
			if started {
				ok = false
			} else if ok {
				env[requested.Name] = requested.Value
			}
			mu.Unlock()
			reply(req, ok)

		case "window-change":
			var size windowChange
			ok := ssh.Unmarshal(req.Payload, &size) == nil
			mu.Lock()
			if ok && term != nil {
				ok = term.Resize(size.Cols, size.Rows) == nil
			}
			mu.Unlock()
			if req.WantReply {
				reply(req, ok)
			}

		case "shell":
			mu.Lock()
			ok := !started && s.user.Shell != nil && s.user.Shell.Enabled
			if ok {
				started = true
				cmd, term, output, err = s.startCommand(ctx, channel, "", false, ptyCfg, env)
				ok = err == nil
			}
			mu.Unlock()
			reply(req, ok)
			if ok {
				go waitCommand(channel, cmd, term, output, s.user.Name, "shell")
			}

		case "exec":
			var requested execRequest
			mu.Lock()
			ok := !started && s.user.Shell != nil && s.user.Shell.Enabled && s.user.Shell.AllowExec &&
				ssh.Unmarshal(req.Payload, &requested) == nil
			if ok {
				started = true
				cmd, term, output, err = s.startCommand(ctx, channel, requested.Command, true, ptyCfg, env)
				ok = err == nil
			}
			mu.Unlock()
			reply(req, ok)
			if ok {
				slog.Info("ssh command started", "user", s.user.Name, "command", requested.Command)
				go waitCommand(channel, cmd, term, output, s.user.Name, requested.Command)
			}

		case "subsystem":
			var requested subsystemRequest
			mu.Lock()
			ok := !started && requested.Name == "" && ssh.Unmarshal(req.Payload, &requested) == nil &&
				requested.Name == "sftp" && s.user.SFTP != nil && s.user.SFTP.Enabled
			if ok {
				started = true
			}
			mu.Unlock()
			reply(req, ok)
			if ok {
				go s.serveSFTP(channel)
			}

		case "signal":
			mu.Lock()
			ok := cmd != nil && cmd.Process != nil
			if ok {
				ok = cmd.Process.Kill() == nil
			}
			mu.Unlock()
			reply(req, ok)

		default:
			reply(req, false)
		}
	}
}

func (s *connectionSession) startCommand(
	ctx context.Context,
	channel ssh.Channel,
	command string,
	isExec bool,
	ptyCfg *ptyRequest,
	env map[string]string,
) (*exec.Cmd, terminal, <-chan struct{}, error) {
	parts, err := shellquote.Split(s.user.Shell.Command)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("parse shell command: %w", err)
	}
	if len(parts) == 0 {
		parts = []string{defaultShell()}
	}
	args := append([]string(nil), parts[1:]...)
	if isExec {
		args = append(args, "-c", command)
	}

	cmd := exec.CommandContext(ctx, parts[0], args...) //nolint:gosec // explicitly configured SSH shell
	for key, value := range env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	if ptyCfg != nil && ptyCfg.Term != "" && !strings.ContainsRune(ptyCfg.Term, '\x00') {
		cmd.Env = append(cmd.Env, "TERM="+ptyCfg.Term)
	}
	if len(cmd.Env) > 0 {
		cmd.Env = append(os.Environ(), cmd.Env...)
	}
	if err := setCommandUser(cmd, s.user.Shell.OSUser); err != nil {
		return nil, nil, nil, err
	}

	if ptyCfg != nil {
		term, err := startPTY(cmd, ptyCfg.Cols, ptyCfg.Rows)
		if err != nil {
			return nil, nil, nil, err
		}
		output := make(chan struct{})
		go func() { _, _ = io.Copy(term, channel) }()
		go func() {
			defer close(output)
			_, _ = io.Copy(channel, term)
		}()
		return cmd, term, output, nil
	}

	cmd.Stdin = channel
	cmd.Stdout = channel
	cmd.Stderr = channel.Stderr()
	if err := cmd.Start(); err != nil {
		return nil, nil, nil, err
	}
	return cmd, nil, nil, nil
}

// ptyDrainTimeout bounds how long to wait for buffered PTY output after the
// command exits, in case a background process keeps the terminal open.
const ptyDrainTimeout = 2 * time.Second

func waitCommand(channel ssh.Channel, cmd *exec.Cmd, term terminal, output <-chan struct{}, user, operation string) {
	err := cmd.Wait()
	if output != nil {
		select {
		case <-output:
		case <-time.After(ptyDrainTimeout):
		}
	}
	if term != nil {
		_ = term.Close()
	}
	status := uint32(0)
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if code := exitErr.ExitCode(); code >= 0 {
			status = uint32(code)
		} else {
			status = 255
		}
	} else if err != nil {
		status = 255
	}
	_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{status}))
	_ = channel.Close()
	slog.Info("ssh command finished", "user", user, "operation", operation, "exit_status", status)
}

func (s *connectionSession) serveSFTP(channel ssh.Channel) {
	options := make([]sftp.ServerOption, 0, 2)
	if s.user.SFTP.Directory != "" {
		options = append(options, sftp.WithServerWorkingDirectory(s.user.SFTP.Directory))
	}
	if s.user.SFTP.ReadOnly {
		options = append(options, sftp.ReadOnly())
	}
	server, err := sftp.NewServer(channel, options...)
	if err == nil {
		err = server.Serve()
	}
	if server != nil {
		_ = server.Close()
	}
	_ = channel.Close()
	if err != nil && !errors.Is(err, io.EOF) {
		slog.Warn("sftp session failed", "user", s.user.Name, "error", err)
	}
}

func reply(req *ssh.Request, ok bool) {
	if req.WantReply {
		_ = req.Reply(ok, nil)
	}
}
