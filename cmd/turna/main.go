package main

import (
	"context"
	"sync"

	"github.com/worldline-go/initializer"
	"github.com/worldline-go/logz"

	"github.com/rakunlabs/turna/cmd/turna/args"
	"github.com/rakunlabs/turna/internal/config"
)

var (
	// populated on build step
	version = "v0.0.0"
	commit  = "?"
	date    = ""
)

func main() {
	config.BuildVars.Version = version
	config.BuildVars.Date = date
	config.BuildVars.Commit = commit

	initializer.Init(
		run,
		initializer.WithInitLog(false),
		initializer.WithOptionsLogz(logz.WithCaller(false)),
	)
}

func run(ctx context.Context, _ *sync.WaitGroup) error {
	return args.Execute(ctx)
}
