package service

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/kballard/go-shellquote"
	"github.com/rakunlabs/turna/pkg/render"
	"github.com/rakunlabs/turna/pkg/runner"
	"github.com/rytsh/liz/loader"
)

type Service struct {
	// Name of service, must be unique.
	Name string `cfg:"name"`
	// Path of service, command run inside this path.
	Path string `cfg:"path"`
	// Command is the command to run with args, gotemplate enabled.
	Command string `cfg:"command"`
	// Env GoTemplate and Sprig functions are available.
	Env map[string]interface{} `cfg:"env"`
	// EnvValues is a list of environment variables path from exported config.
	EnvValues []string `cfg:"env_values"`
	// Inherit environment variables, default is false.
	InheritEnv bool `cfg:"inherit_env"`
	// User is the user to run command, id:group or just id.
	User string `cfg:"user"`
	// Filters is a function to filter stdout.
	Filters [][]byte `cfg:"filters"`
	// FiltersValues is a list of filter variables path from exported config.
	FiltersValues []string `cfg:"filters_values"`

	// Order is the order of service to run.
	//
	// If same order set, they will run in parallel.
	Order int `cfg:"order"`
	// Depends is a list of service names to depend on.
	//
	// Order is ignoring if depend is set.
	Depends []string `cfg:"depends"`
	// AllowFailure is a flag to allow failure of service.
	AllowFailure bool `cfg:"allow_failure"`

	// filters is internal usage to combine filters and filters_values.
	filters [][]byte
	mutex   sync.RWMutex
}

func (s *Service) SetFilters() {
	filterX := s.Filters

	for _, path := range s.FiltersValues {
		if vInner, ok := loader.InnerPath(path, render.GlobalRender.Data).([]interface{}); ok {
			for _, val := range vInner {
				rV, err := render.GlobalRender.Execute(val)
				if err != nil {
					slog.Warn("failed to render filter value "+s.Name, "err", err.Error())

					continue
				}

				filterX = append(filterX, []byte(rV))
			}
		}
	}

	s.mutex.Lock()
	s.filters = filterX
	s.mutex.Unlock()
}

func (s *Service) Register() error {
	filter := func(b []byte) bool {
		// reading s.filters is not thread safe, so we need to lock it
		s.mutex.RLock()
		defer s.mutex.RUnlock()

		for _, f := range s.filters {
			if bytes.Contains(b, f) {
				return false
			}
		}

		return true
	}

	env, err := s.GetEnv(s.Env, s.InheritEnv, s.EnvValues)
	if err != nil {
		return err
	}

	renderedCommand, err := render.GlobalRender.Execute(s.Command)
	if err != nil {
		return fmt.Errorf("failed to render command %s: %w", s.Name, err)
	}

	commands, err := shellquote.Split(string(renderedCommand))
	if err != nil {
		return fmt.Errorf("failed to parse command %s: %w", s.Name, err)
	}

	c := &runner.Command{
		Name:         s.Name,
		Path:         s.Path,
		Command:      commands,
		Filter:       filter,
		Env:          env,
		AllowFailure: s.AllowFailure,
		Order:        s.Order,
		Depends:      s.Depends,
		User:         s.User,
	}

	if err := runner.GlobalReg.Add(c); err != nil {
		return err
	}

	slog.Info(fmt.Sprintf("added service [%s] to registry", s.Name))

	return nil
}

type Services []Service

func (s Services) Run(ctx context.Context) error {
	for i := range s {
		if err := s[i].Register(); err != nil {
			return err
		}
	}

	return runner.GlobalReg.Run(ctx)
}
