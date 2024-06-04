package runner

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/rakunlabs/turna/pkg/filter"
)

var ErrRunInit = fmt.Errorf("run init error")

type Command struct {
	proc         *os.Process
	wgProg       sync.WaitGroup
	Name         string
	Path         string
	Env          []string
	Filter       func([]byte) bool
	Command      []string
	AllowFailure bool
	Order        int
	Depends      []string
	trigger      []string
	killLock     sync.Mutex
	killStarted  bool
	User         string

	dependLock sync.Mutex
	dependGet  map[string]struct{}

	stdin  *os.File
	stdout *os.File
	stderr *os.File
}

func (c *Command) DependecyTrigger(ctx context.Context, name string) error {
	c.dependLock.Lock()
	defer c.dependLock.Unlock()

	if c.dependGet == nil {
		c.dependGet = make(map[string]struct{})
	}

	c.dependGet[name] = struct{}{}

	for _, depend := range c.Depends {
		if _, ok := c.dependGet[depend]; ok {
			continue
		}

		return nil
	}

	// all dependecy comes, run command
	return c.Run(ctx)
}

func (c *Command) start(ctx context.Context, wg *sync.WaitGroup) (*os.Process, error) {
	var err error

	// command with new path
	var cmdx string

	// set path of the process
	if c.Path != "" {
		if path, err := filepath.Abs(c.Path); err == nil {
			c.Path = path
		}
	}

	commandPath := c.Command[0]

	if strings.Contains(commandPath, "/") && filepath.IsLocal(commandPath) {
		if c.Path != "" {
			commandPath = filepath.Join(c.Path, commandPath)
		}
	}

	if cmdx, err = exec.LookPath(commandPath); err != nil {
		return nil, fmt.Errorf("lookpath error; %w", err)
	}

	slog.Debug(fmt.Sprintf("starting [%s] command", cmdx))

	stdin := c.stdin
	if stdin == nil {
		stdin = os.Stdin
	}

	stdout := c.stdout
	if stdout == nil {
		stdout = os.Stdout
	}

	stderr := c.stderr
	if stderr == nil {
		stderr = os.Stderr
	}

	if c.Filter != nil {
		slog.Info(fmt.Sprintf("filtering [%s]", c.Name))
		// filter for stdout
		filteredStdout := filter.FileFilter{To: stdout, Filter: c.Filter}

		stdout, err = filteredStdout.Start(ctx, wg)
		if err != nil {
			return nil, fmt.Errorf("cannot start filtered stdout: %w", err)
		}

		// filter for stderr
		filteredStderr := filter.FileFilter{To: stderr, Filter: c.Filter}

		stderr, err = filteredStderr.Start(ctx, wg)
		if err != nil {
			return nil, fmt.Errorf("cannot start filtered stderr: %w", err)
		}
	}

	procAttr := new(os.ProcAttr)
	procAttr.Files = []*os.File{
		stdin,
		stdout,
		stderr,
	}

	procAttr.Env = c.Env
	procAttr.Dir = c.Path

	sys, err := sysProcAttr(c.User)
	if err != nil {
		return nil, err
	}
	procAttr.Sys = sys

	var p *os.Process

	p, err = os.StartProcess(cmdx, c.Command, procAttr)
	if err != nil {
		return nil, fmt.Errorf("process cannot run; %w", err)
	}

	// listen ctx cancel
	wg.Add(1)
	go func() {
		defer wg.Done()

		<-ctx.Done()
		c.Kill()
	}()

	return p, nil
}

func (c *Command) Run(ctx context.Context) error {
	if c.proc != nil {
		return fmt.Errorf("process already running: %w", ErrRunInit)
	}

	if len(c.Command) == 0 {
		return fmt.Errorf("doesn't given any command: %w", ErrRunInit)
	}

	ctx, ctxCancel := context.WithCancel(ctx)
	defer ctxCancel()

	var err error

	slog.Info(fmt.Sprintf("starting [%s] command", c.Name))
	c.killLock.Lock()
	c.proc, err = c.start(ctx, &c.wgProg)
	c.killLock.Unlock()
	if err != nil {
		return err
	}

	c.wgProg.Add(1)
	defer c.wgProg.Done()

	defer func() {
		c.killLock.Lock()
		c.proc = nil
		c.killLock.Unlock()
	}()

	state, err := c.proc.Wait()
	if err != nil {
		slog.Warn(fmt.Sprintf("process [%s] wait", c.Name), "err", err)
	}

	exitCode := state.ExitCode()
	if exitCode != 0 {
		slog.Warn(fmt.Sprintf("process [%s] exited with code %d", c.Name, exitCode))
		if !c.AllowFailure {
			return fmt.Errorf("process [%s] exited with code %d", c.Name, exitCode)
		}
	} else {
		slog.Info(fmt.Sprintf("process [%s] exited with code %d", c.Name, exitCode))
	}

	return nil
}

// Kill the kill command.
func (c *Command) Kill() {
	var v *os.Process

	c.killLock.Lock()
	if c.killStarted {
		c.killLock.Unlock()

		return
	}

	v = c.proc
	c.killStarted = true
	c.killLock.Unlock()

	defer func() {
		c.killStarted = false
	}()

	if v != nil {
		slog.Warn(fmt.Sprintf("killing process [%s] [%d]", c.Name, v.Pid))

		if err := terminateProcess(v.Pid); err != nil {
			slog.Error(fmt.Sprintf("failed to kill process [%s] [%d]", c.Name, v.Pid), "err", err)
		}

		c.wgProg.Wait()

		return
	}
}

// Restart is kill command after that run again that command.
func (c *Command) Restart(ctx context.Context) error {
	// minus PID to send signal child PIDs
	slog.Info(fmt.Sprintf("restarting [%s] command", c.Name))
	c.Kill()

	return c.Run(ctx)
}
