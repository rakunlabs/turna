package args

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/worldline-go/turna/internal/config"
	"github.com/worldline-go/turna/internal/service"
	"github.com/worldline-go/turna/pkg/runner"

	"github.com/rs/zerolog/log"
	load "github.com/rytsh/liz/loader"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/worldline-go/igconfig"
	"github.com/worldline-go/igconfig/loader"
	"github.com/worldline-go/logz"
)

var ErrShutdown = errors.New("shutting down signal received")

type Build struct {
	Version string
	Commit  string
	Date    string
}

var BuildVars = Build{}

type overrideHold struct {
	Memory *string
	Value  string
}

var rootCmd = &cobra.Command{
	Use:   "turna",
	Short: "process manager",
	Long:  fmt.Sprintf("%s\nturna extends functionality of services", config.GetBanner()),
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if err := logz.SetLogLevel(config.Application.LogLevel); err != nil {
			return err //nolint:wrapcheck // no need
		}

		return nil
	},
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		// load configuration
		if err := loadConfig(cmd.Context(), cmd.Flags().Visit); err != nil {
			return err
		}

		if err := runRoot(cmd.Context()); err != nil {
			return err
		}

		return nil
	},
}

func Execute(ctx context.Context) error {
	setFlags()

	rootCmd.Version = BuildVars.Version
	rootCmd.Long = fmt.Sprintf(
		"%s\nversion:[%s] commit:[%s] buildDate:[%s]",
		rootCmd.Long, BuildVars.Version, BuildVars.Commit, BuildVars.Date,
	)

	return rootCmd.ExecuteContext(ctx) //nolint:wrapcheck // no need
}

func setFlags() {
	rootCmd.PersistentFlags().StringVarP(&config.Application.LogLevel, "log-level", "l", config.Application.LogLevel, "log level")
	rootCmd.PersistentFlags().BoolVar(&config.LoadConfig.ConfigSet.Consul, "config-consul", config.LoadConfig.ConfigSet.Consul, "first config get in consul")
	rootCmd.PersistentFlags().BoolVar(&config.LoadConfig.ConfigSet.Vault, "config-vault", config.LoadConfig.ConfigSet.Vault, "first config get in vault")
	rootCmd.PersistentFlags().BoolVar(&config.LoadConfig.ConfigSet.File, "config-file", config.LoadConfig.ConfigSet.File, "first config get in file")
}

// override function hold first values of definitions.
// Use with pflag visit function.
func override(ow map[string]overrideHold) {
	ow["log-level"] = overrideHold{&config.Application.LogLevel, config.Application.LogLevel}
}

func loadConfig(ctx context.Context, visit func(fn func(*pflag.Flag))) error {
	overrideValues := make(map[string]overrideHold)
	override(overrideValues)

	logConfig := log.With().Str("component", "config").Logger()
	ctxConfig := logConfig.WithContext(ctx)

	loaders := []loader.Loader{}

	envLoader := &loader.Env{}

	if err := igconfig.LoadWithLoadersWithContext(ctxConfig, "", &config.LoadConfig, envLoader); err != nil {
		return fmt.Errorf("unable to load prefix settings: %v", err)
	}

	log.Info().Msgf("config loaded from %+v", config.LoadConfig)

	loader.ConsulConfigPathPrefix = config.LoadConfig.Prefix.Consul
	loader.VaultSecretBasePath = config.LoadConfig.Prefix.Vault
	loader.VaultSecretAdditionalPaths = nil

	if config.LoadConfig.ConfigSet.Consul {
		loaders = append(loaders, &loader.Consul{})
	}

	if config.LoadConfig.ConfigSet.Vault && config.LoadConfig.Prefix.Vault != "" {
		loaders = append(loaders, &loader.Vault{})
	}

	if config.LoadConfig.ConfigSet.File {
		loaders = append(loaders, &loader.File{})
	}

	loaders = append(loaders, envLoader)

	if err := igconfig.LoadWithLoadersWithContext(ctxConfig, config.LoadConfig.AppName, &config.Application, loaders...); err != nil {
		return fmt.Errorf("unable to load configuration settings: %v", err)
	}

	// override used cmd values
	visit(func(f *pflag.Flag) {
		if v, ok := overrideValues[f.Name]; ok {
			*v.Memory = v.Value
		}
	})

	// set log again to get changes
	if err := logz.SetLogLevel(config.Application.LogLevel); err != nil {
		return err //nolint:wrapcheck // no need
	}

	// print loaded object
	log.Info().Object("config", igconfig.Printer{Value: config.Application}).Msg("loaded config")

	return nil
}

func runRoot(ctxParent context.Context) (err error) {
	// appname and version
	log.Info().Msgf("%s [%s]", strings.ToTitle(config.AppName), BuildVars.Version)

	wg := &sync.WaitGroup{}
	defer wg.Wait()

	ctx, ctxCancel := context.WithCancel(ctxParent)
	defer ctxCancel()

	// create global registry
	wgRunner := &sync.WaitGroup{}

	// add store runner
	runner.NewStoreReg(ctx, wgRunner).SetAsGlobal()

	wg.Add(1)

	go func() {
		defer wg.Done()

		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

		select {
		case <-sig:
			log.Warn().Msg("received shutdown signal")
			ctxCancel()

			if err != nil {
				err = ErrShutdown
			}
		case <-ctx.Done():
			log.Warn().Msg("turna closing")
		}

		runner.GlobalReg.KillAll()

		// add ErrShutdown if exit code is not 0
		if err != nil && !runner.GlobalReg.IsExitCodeZero() {
			err = ErrShutdown
		}
	}()

	// this function will be called after all configs are loaded and dynamically changes
	call := func(_ context.Context, _ string, data map[string]interface{}) {
		// set global data
		service.Data = data

		// set service filters
		for i := range config.Application.Services {
			config.Application.Services[i].SetFilters()
		}
	}

	// load configurations
	if err := config.Application.Loads.Load(load.SetLogToCtx(ctx, logz.AdapterKV{Log: log.Logger}), wg, nil, call); err != nil {
		return err
	}

	// run services
	if err := config.Application.Services.Run(); err != nil {
		return err
	}

	wgRunner.Wait()

	return nil
}
