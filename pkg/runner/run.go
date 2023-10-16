package runner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/rs/zerolog/log"

	"github.com/worldline-go/turna/pkg/filter"
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

	if cmdx, err = exec.LookPath(c.Command[0]); err != nil {
		return nil, fmt.Errorf("lookpath error; %w", err)
	}

	log.Debug().Msgf("starting [%s] command", cmdx)

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
		log.Info().Msgf("filtering [%s]", c.Name)
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

	// set path of the process
	if c.Path != "" {
		if path, err := filepath.Abs(c.Path); err != nil {
			c.Path = path
		}
	}

	procAttr.Dir = c.Path
	procAttr.Sys = &syscall.SysProcAttr{Setpgid: true}

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

	log.Info().Msgf("starting [%s] command", c.Name)
	c.proc, err = c.start(ctx, &c.wgProg)
	if err != nil {
		return err
	}
	defer func() {
		c.proc = nil
	}()

	c.wgProg.Add(1)
	defer c.wgProg.Done()

	state, err := c.proc.Wait()
	if err != nil {
		log.Warn().Err(err).Msg("process wait")
	}

	exitCode := state.ExitCode()
	if exitCode != 0 {
		log.Warn().Msgf("process [%s] exited with code %d", c.Name, exitCode)
		if !c.AllowFailure {
			return fmt.Errorf("process [%s] exited with code %d", c.Name, exitCode)
		}
	} else {
		log.Info().Msgf("process [%s] exited with code %d", c.Name, exitCode)
	}

	return nil
}

// Kill the kill command.
func (c *Command) Kill() {
	if c.proc != nil {
		log.Warn().Msgf("killing process [%s]", c.Name)

		if err := syscall.Kill(-c.proc.Pid, syscall.SIGINT); err != nil {
			log.Logger.Error().Err(err).Msg("syscall kill failed")
		}

		c.wgProg.Wait()

		return
	}
}

// Restart is kill command after that run again that command.
func (c *Command) Restart(ctx context.Context) error {
	// minus PID to send signal child PIDs
	log.Info().Msg("restarting process..")
	c.Kill()

	return c.Run(ctx)
}
