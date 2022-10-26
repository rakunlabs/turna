package runner

import (
	"context"
	"sync"

	"github.com/rs/zerolog/log"
)

// GlobalReg using in server requests.
// Don't forget to run KillAll before exit the program.
var GlobalReg *StoreReg

// StoreReg hold commands.
type StoreReg struct {
	reg map[string]*Command
	wg  *sync.WaitGroup
	ctx context.Context
	rwm sync.RWMutex
}

func (s *StoreReg) Add(command *Command) *StoreReg {
	command.SetWaitGroup(s.wg)
	command.SetRegistry(s)

	if s.ctx != nil {
		ctx, cancelFunc := context.WithCancel(s.ctx)
		command.SetContext(ctx, cancelFunc)
	}

	s.rwm.Lock()
	defer s.rwm.Unlock()

	s.reg[command.Name] = command

	return s
}

func (s *StoreReg) Del(name string) *StoreReg {
	s.rwm.Lock()
	defer s.rwm.Unlock()

	delete(s.reg, name)

	return s
}

func (s *StoreReg) Get(name string) *Command {
	s.rwm.RLock()
	defer s.rwm.RUnlock()

	return s.reg[name]
}

func (s *StoreReg) KillAll() {
	log.Warn().Msg("killing all process")

	for key := range s.reg {
		s.reg[key].Kill()
	}

	log.Warn().Msg("killing all process done")
}

func (s *StoreReg) SetAsGlobal() *StoreReg {
	GlobalReg = s

	return s
}

func (s *StoreReg) RunAll() error {
	for key := range s.reg {
		log.Info().Msgf("starting [%s]", key)
		if err := s.reg[key].Run(false); err != nil {
			return err
		}
	}

	return nil
}

func (s *StoreReg) IsExitCodeZero() bool {
	for key := range s.reg {
		if s.reg[key].ExitCode != 0 {
			return false
		}
	}

	return true
}

func NewStoreReg(ctx context.Context, wg *sync.WaitGroup) *StoreReg {
	return &StoreReg{
		reg: make(map[string]*Command),
		wg:  wg,
		ctx: ctx,
	}
}
