package args

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/rakunlabs/into"
	"github.com/rakunlabs/logi"
	"github.com/rakunlabs/turna/internal/config"
	"github.com/rakunlabs/turna/pkg/render"
	"github.com/rakunlabs/turna/pkg/runner"
	"github.com/rakunlabs/turna/pkg/server/http"
	serverReg "github.com/rakunlabs/turna/pkg/server/registry"
	"github.com/worldline-go/struct2"

	"github.com/rs/zerolog/log"
	load "github.com/rytsh/liz/loader"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/worldline-go/igconfig"
	"github.com/worldline-go/igconfig/loader"
	"github.com/worldline-go/logz"
)

type overrideHold struct {
	Memory *string
	Value  string
}

var rootCmd = &cobra.Command{
	Use:   "turna",
	Short: "process manager",
	Long:  config.GetBanner() + "\nturna extends functionality of services",
	PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
		if err := logi.SetLogLevel(config.Application.LogLevel); err != nil {
			return err //nolint:wrapcheck // no need
		}

		return nil
	},
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, _ []string) error {
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

	rootCmd.Version = config.BuildVars.Version
	rootCmd.Long = fmt.Sprintf(
		"%s\nversion:[%s] commit:[%s] buildDate:[%s]",
		rootCmd.Long, config.BuildVars.Version, config.BuildVars.Commit, config.BuildVars.Date,
	)

	rootCmd.AddCommand(apiCmd)

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
		return fmt.Errorf("unable to load prefix settings: %w", err)
	}

	slog.Info("config loading from", "config", config.LoadConfig)

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
		return fmt.Errorf("unable to load configuration settings: %w", err)
	}

	// override used cmd values
	visit(func(f *pflag.Flag) {
		if v, ok := overrideValues[f.Name]; ok {
			*v.Memory = v.Value
		}
	})

	// set log again to get changes
	if err := logi.SetLogLevel(config.Application.LogLevel); err != nil {
		return err //nolint:wrapcheck // no need
	}

	if slog.Default().Enabled(ctx, slog.LevelDebug) {
		decoder := struct2.Decoder{TagName: "cfg", OmitNilPtr: true}

		m := decoder.Map(config.Application)
		slog.Debug("loaded config", "config", m)
	}

	return nil
}

func runRoot(ctx context.Context) error {
	// appname and version
	logi.Log(fmt.Sprintf(
		"TURNA [%s] [%s] buildCommit=[%s] buildDate=[%s]",
		config.LoadConfig.AppName, config.BuildVars.Version,
		config.BuildVars.Commit, config.BuildVars.Date,
	))

	wg := &sync.WaitGroup{}
	defer wg.Wait()

	// add store runner
	runner.NewStoreReg(wg).SetAsGlobal()
	into.ShutdownAdd(into.FnWarp(runner.GlobalReg.KillAll), "runner")

	// this function will be called after all configs are loaded and dynamically changes
	call := func(_ context.Context, _ string, data map[string]interface{}) {
		render.Data = data

		// set service filters
		for i := range config.Application.Services {
			config.Application.Services[i].SetFilters()
		}

		// notify
		slog.Info("dynamic config loaded")
	}

	// load configurations
	if err := config.Application.Loads.Load(load.SetLogToCtx(ctx, logz.AdapterKV{Log: log.Logger}), wg, nil, call); err != nil {
		return err
	}

	// preprocess
	if err := config.Application.Preprocess.Run(ctx); err != nil {
		return err
	}

	// print for log to starting program
	if err := Print(); err != nil {
		return err
	}

	// server
	into.ShutdownAdd(into.FnWarp(serverReg.GlobalReg.Shutdown), "server registry")

	if config.Application.Server.LoadValue != "" {
		if err := config.Decode(
			render.Data[config.Application.Server.LoadValue],
			&config.Application.Server,
		); err != nil {
			return fmt.Errorf("unable to load server config from load_value: %w", err)
		}
	}

	http.ServerInfo = config.AppName + " [" + config.BuildVars.Version + "]"
	if err := config.Application.Server.Run(ctx, wg); err != nil {
		into.CtxCancel()

		return err
	}

	// run services
	if err := config.Application.Services.Run(ctx); err != nil {
		into.CtxCancel()

		return err
	}

	return nil
}

func Print() error {
	if config.Application.Print == "" {
		return nil
	}

	vPrint, err := render.Execute(config.Application.Print)
	if err != nil {
		return err
	}

	slog.Info(string(vPrint))

	return nil
}
