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

type Command struct {
	ctx       context.Context
	TriggerFn func(context.Context) error
	proc      *os.Process
	wg        *sync.WaitGroup
	waitChan  chan struct{}
	registry  *StoreReg
	ctxCancel context.CancelFunc
	Name      string
	Path      string
	Env       []string
	Filter    func([]byte) bool
	closeFunc func()
	Command   []string
	ExitCode  int
}

// SetWaitGroup is for shutdown service gracefully.
func (c *Command) SetWaitGroup(wg *sync.WaitGroup) {
	c.wg = wg
}

// SetRegistry to reach command to registry struct.
func (c *Command) SetRegistry(registry *StoreReg) {
	c.registry = registry
}

// SetContext for killing command.
func (c *Command) SetContext(ctx context.Context, ctxCancel context.CancelFunc) {
	c.ctx = ctx
	c.ctxCancel = ctxCancel
}

func (c *Command) start() (*os.Process, error) {
	var err error

	// command with new path
	var cmdx string

	if cmdx, err = exec.LookPath(c.Command[0]); err != nil {
		return nil, fmt.Errorf("lookpath error; %w", err)
	}

	log.Debug().Msgf("starting [%s] command", cmdx)

	stdoutFile := os.Stdout
	stderrFile := os.Stderr

	if c.Filter != nil {
		log.Info().Msgf("filtering [%s]", c.Name)
		// filter for stdout
		filteredStdout := filter.FileFilter{To: os.Stdout, Filter: c.Filter}

		stdoutFile, err = filteredStdout.Start()
		if err != nil {
			return nil, fmt.Errorf("cannot start filtered stdout: %w", err)
		}

		// filter for stderr
		filteredStderr := filter.FileFilter{To: os.Stderr, Filter: c.Filter}

		stderrFile, err = filteredStderr.Start()
		if err != nil {
			return nil, fmt.Errorf("cannot start filtered stderr: %w", err)
		}

		c.closeFunc = func() {
			filteredStdout.Close()
			filteredStderr.Close()
		}
	}

	procAttr := new(os.ProcAttr)
	procAttr.Files = []*os.File{
		os.Stdin,
		stdoutFile,
		stderrFile,
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

	return p, nil
}

//nolint:cyclop // required to be together
func (c *Command) Run(runTrigger bool) error {
	if c.proc != nil {
		return fmt.Errorf("process already running")
	}

	if len(c.Command) == 0 {
		return fmt.Errorf("doesn't given any command")
	}

	// run init function if exists
	if c.TriggerFn != nil && runTrigger {
		var ctx context.Context
		if c.registry != nil {
			ctx = c.registry.ctx
		}

		if ctx == nil {
			ctx = context.Background()
		}

		_ = c.TriggerFn(ctx)
	}

	var err error

	c.proc, err = c.start()
	if err != nil {
		return err
	}

	if c.wg != nil {
		c.wg.Add(1)
	}

	c.waitChan = make(chan struct{}, 1)

	go func() {
		state, err := c.proc.Wait()
		if err != nil {
			log.Warn().Err(err).Msg("process wait")
		}

		log.Info().Msgf("closed [%s]", c.Name)

		if state.ExitCode() != 0 {
			log.Warn().Msgf("process [%s] exited with code %d", c.Name, state.ExitCode())
			c.ExitCode = state.ExitCode()
		}

		c.proc = nil
		close(c.waitChan)

		if c.wg != nil {
			c.wg.Done()
		}
	}()

	return nil
}

// Kill the kill command.
func (c *Command) Kill() {
	log.Warn().Msgf("killing process [%s]", c.Name)

	if c.ctxCancel != nil {
		c.ctxCancel()
	}

	if c.proc != nil {
		if err := syscall.Kill(-c.proc.Pid, syscall.SIGINT); err != nil {
			log.Logger.Error().Err(err)
		}

		<-c.waitChan

		// close additional started process
		if c.closeFunc != nil {
			c.closeFunc()
		}

		return
	}

	log.Warn().Msgf("process already not running [%s]", c.Name)
}

// Restart is kill command after that run again that command.
func (c *Command) Restart(runInit bool) error {
	// minus PID to send signal child PIDs
	log.Info().Msg("restarting process..")
	c.Kill()

	return c.Run(runInit)
}
