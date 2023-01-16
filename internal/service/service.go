package service

import (
	"bytes"
	"fmt"

	"github.com/kballard/go-shellquote"
	"github.com/rs/zerolog/log"
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
	// Inherit environment variables, default is false.
	InheritEnv bool `cfg:"inherit_env"`
	// Filters is a function to filter stdout.
	Filters [][]byte
}

func (s *Service) Add() error {
	var filter func(b []byte) bool

	if s.Filters != nil {
		filter = func(b []byte) bool {
			for _, f := range s.Filters {
				if bytes.Contains(b, f) {
					return false
				}
			}
			return true
		}
	}

	env, err := GetEnv(s.Env, s.InheritEnv)
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
		if err := s[i].Add(); err != nil {
			return err
		}
	}

	return runner.GlobalReg.RunAll()
}
