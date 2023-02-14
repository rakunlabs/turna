package main

import (
	"context"
	"errors"
	"os"

	"github.com/rs/zerolog/log"
	"github.com/worldline-go/logz"

	"github.com/worldline-go/turna/cmd/turna/args"
)

var (
	// populated on build step
	version = "v0.0.0"
	commit  = "?"
	date    = ""
)

func main() {
	logz.InitializeLog(logz.WithCaller(false))

	args.BuildVars.Version = version
	args.BuildVars.Date = date
	args.BuildVars.Commit = commit

	var err error

	defer func() {
		// recover from panic if one occured to prevent os.Exit
		if r := recover(); r != nil {
			log.Fatal().Msgf("%v", r)
		}

		if err != nil {
			os.Exit(1)
		}
	}()

	if err = args.Execute(context.Background()); err != nil {
		if !errors.Is(err, args.ErrShutdown) {
			log.Error().Err(err).Msg("failed to execute command")
		}
	}
}
