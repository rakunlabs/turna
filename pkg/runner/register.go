package runner

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"sync"
)

// GlobalReg using in server requests.
// Don't forget to run KillAll before exit the program.
var GlobalReg *StoreReg

// StoreReg hold commands.
type StoreReg struct {
	reg map[string]*Command
	// order just hold ordered command names and add commands to which depends on.
	order []*OrderCommand
	wg    *sync.WaitGroup
	rwm   sync.RWMutex
}

type OrderCommand struct {
	Order    int
	Names    []string
	wg       *sync.WaitGroup
	StoreReg *StoreReg
}

func (o *OrderCommand) Run(ctxParent context.Context, wg *sync.WaitGroup) error {
	o.wg = &sync.WaitGroup{}
	ctx, ctxCancel := context.WithCancel(ctxParent)
	defer ctxCancel()

	var errStore []error

	for _, name := range o.Names {
		o.wg.Add(1)
		go func(name string) {
			defer o.wg.Done()

			// run command
			if err := o.StoreReg.reg[name].Run(ctx); err != nil {
				slog.Error(fmt.Sprintf("failed command [%s]", name), "err", err.Error())

				errStore = append(errStore, err)

				ctxCancel()

				return
			}

			// run dependencies
			wg.Add(1)
			go func() {
				defer wg.Done()

				ctx, ctxCancel := context.WithCancel(ctxParent)
				defer ctxCancel()

				for _, depend := range o.StoreReg.reg[name].trigger {
					slog.Info(fmt.Sprintf("command [%s] dependecy trigger [%s]", name, depend))

					if err := o.StoreReg.reg[depend].DependecyTrigger(ctx, name); err != nil {
						slog.Error(fmt.Sprintf("failed command [%s]", name), "err", err.Error())

						errStore = append(errStore, err)

						ctxCancel()

						return
					}
				}
			}()
		}(name)
	}

	o.wg.Wait()

	slog.Info(fmt.Sprintf("order [%d] done", o.Order))

	if len(errStore) > 0 {
		return fmt.Errorf("command [%s] failed: %s", o.Names, errStore)
	}

	return nil
}

func (s *StoreReg) dependecySet() error {
	s.rwm.Lock()
	defer s.rwm.Unlock()

	// set dependecy
	for name := range s.reg {
		for _, depend := range s.reg[name].Depends {
			if _, ok := s.reg[depend]; !ok {
				return fmt.Errorf("dependecy [%s] not found for [%s]", depend, name)
			}

			s.reg[depend].trigger = append(s.reg[depend].trigger, name)
		}
	}

	dependecy := []*OrderCommand{}

	for key := range s.reg {
		if len(s.reg[key].Depends) > 0 {
			continue
		}

		// find the exist order
		index := slices.IndexFunc(dependecy, func(i *OrderCommand) bool {
			return i.Order == s.reg[key].Order
		})

		if index == -1 {
			dependecy = append(dependecy, &OrderCommand{
				StoreReg: s,
				Order:    s.reg[key].Order,
				Names:    []string{key},
			})

			continue
		}

		dependecy[index].Names = append(dependecy[index].Names, key)
	}

	// sort for minimum order
	sort.Slice(dependecy, func(i, j int) bool {
		return dependecy[i].Order < dependecy[j].Order
	})

	s.order = dependecy

	return nil
}

func (s *StoreReg) Add(command *Command) error {
	s.rwm.Lock()
	defer s.rwm.Unlock()

	name := command.Name
	if _, ok := s.reg[name]; ok {
		return fmt.Errorf("command with name [%s] already exists", name)
	}

	s.reg[name] = command

	return nil
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
	slog.Warn("killing all process")

	for key := range s.reg {
		s.reg[key].Kill()
	}

	slog.Warn("killing all process done")
}

func (s *StoreReg) SetAsGlobal() *StoreReg {
	GlobalReg = s

	return s
}

func (s *StoreReg) Run(ctx context.Context) error {
	if err := s.dependecySet(); err != nil {
		return err
	}

	for i := range s.order {
		if err := s.order[i].Run(ctx, s.wg); err != nil {
			return err
		}
	}

	return nil
}

func NewStoreReg(wg *sync.WaitGroup) *StoreReg {
	return &StoreReg{
		reg: make(map[string]*Command),
		wg:  wg,
	}
}
