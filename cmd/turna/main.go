package main

import (
	"github.com/rakunlabs/into"
	"github.com/rakunlabs/logi"
	"github.com/worldline-go/turna/cmd/turna/args"
	"github.com/worldline-go/turna/internal/config"
)

var (
	version = "v0.0.0"
	commit  = "?"
	date    = ""
)

func main() {
	config.BuildVars.Version = version
	config.BuildVars.Date = date
	config.BuildVars.Commit = commit

	into.Init(
		args.Execute,
		into.WithLogger(logi.InitializeLog(logi.WithCaller(false))),
		into.WithMsgf("turna [%s]", version),
		into.WithStartFn(nil),
		into.WithStopFn(nil),
	)
}
