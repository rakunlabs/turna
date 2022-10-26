package runner

import (
	"context"
	"sync"

	"github.com/rs/zerolog/log"
)

type RunChanStruct struct {
	Name string
	Init bool
}

// RunChan trigger to function.
var RunChan chan RunChanStruct

func RunTrigger(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	RunChan = make(chan RunChanStruct)

	defer close(RunChan)

	for {
		select {
		case command := <-RunChan:
			if GlobalReg == nil {
				log.Warn().Msg("globalReg not set")

				continue
			}

			cmd := GlobalReg.Get(command.Name)
			if cmd == nil {
				log.Warn().Msgf("[%s] command not found in registry", command.Name)

				continue
			}

			if err := cmd.Restart(command.Init); err != nil {
				log.Warn().Err(err).Msg("command cannot run")
			}
		case <-ctx.Done():
			return
		}
	}
}
