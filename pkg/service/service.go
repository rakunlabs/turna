package service

import (
	"bytes"
	"fmt"
	"sync"

	"github.com/kballard/go-shellquote"
	"github.com/rs/zerolog/log"
	"github.com/rytsh/liz/loader"
	"github.com/worldline-go/turna/pkg/render"
	"github.com/worldline-go/turna/pkg/runner"
)

type Service struct {
	// Name of service.
	Name string
	// Path of service, command run inside this path.
	Path string
	// Command is the command to run with args.
	Command string
	// GoTemplate and Sprig functions are available.
	Env map[string]interface{}
	// EnvValues is a list of environment variables path from exported config.
	EnvValues []string `cfg:"env_values"`
	// Inherit environment variables, default is false.
	InheritEnv bool `cfg:"inherit_env"`
	// Filters is a function to filter stdout.
	Filters [][]byte
	// FiltersValues is a list of filter variables path from exported config.
	FiltersValues []string `cfg:"filters_values"`

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
					log.Warn().Msgf("failed to render filter value %s: %v", s.Name, err)

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

	commands, err := shellquote.Split(s.Command)
	if err != nil {
		return fmt.Errorf("failed to parse command %s: %w", s.Name, err)
	}

	c := &runner.Command{
		Name:    s.Name,
		Path:    s.Path,
		Command: commands,
		Filter:  filter,
		Env:     env,
	}

	runner.GlobalReg.Add(c)

	log.Info().Msgf("added service [%s] to registry", s.Name)

	return nil
}

type Services []Service

func (s Services) Run() error {
	for i := range s {
		if err := s[i].Register(); err != nil {
			return err
		}
	}

	return runner.GlobalReg.RunAll()
}
