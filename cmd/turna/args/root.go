package args

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"github.com/rakunlabs/chu"
	"github.com/rakunlabs/into"
	"github.com/rakunlabs/logi"
	"github.com/rakunlabs/turna/internal/config"
	"github.com/rakunlabs/turna/pkg/render"
	"github.com/rakunlabs/turna/pkg/runner"
	"github.com/rakunlabs/turna/pkg/server/http"
	serverReg "github.com/rakunlabs/turna/pkg/server/registry"
	"github.com/worldline-go/struct2"

	// External chu loaders (registered via init) so they can be selected by
	// name when the env-provided config set enables them.
	_ "github.com/rakunlabs/chu/loader/external/loaderawssecrets"
	_ "github.com/rakunlabs/chu/loader/external/loaderawsssm"
	_ "github.com/rakunlabs/chu/loader/external/loaderazurekeyvault"
	_ "github.com/rakunlabs/chu/loader/external/loaderconsul"
	_ "github.com/rakunlabs/chu/loader/external/loadergcpparameter"
	_ "github.com/rakunlabs/chu/loader/external/loadergcpsecret"
	_ "github.com/rakunlabs/chu/loader/external/loadervault"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	envAppName = "APP_NAME"
)

var AppName = "turna"

func init() {
	if v := os.Getenv(envAppName); v != "" {
		AppName = v
	}
}

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
}

// override function hold first values of definitions.
// Use with pflag visit function.
func override(ow map[string]overrideHold) {
	ow["log-level"] = overrideHold{&config.Application.LogLevel, config.Application.LogLevel}
}

func loadConfig(ctx context.Context, visit func(fn func(*pflag.Flag))) error {
	overrideValues := make(map[string]overrideHold)
	override(overrideValues)

	if err := chu.Load(ctx, AppName, &config.Application); err != nil {
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
		AppName, config.BuildVars.Version,
		config.BuildVars.Commit, config.BuildVars.Date,
	))

	wg := &sync.WaitGroup{}
	defer wg.Wait()

	// add store runner
	runner.NewStoreReg(wg).SetAsGlobal()
	into.ShutdownAdd(into.FnWarp(runner.GlobalReg.KillAll), "runner")

	// this function will be called after all configs are loaded and dynamically changes
	call := func(_ context.Context, _ string, data map[string]any) {
		render.Data = data

		// set service filters
		for i := range config.Application.Services {
			config.Application.Services[i].SetFilters()
		}

		// notify
		slog.Info("dynamic config loaded")
	}

	// load configurations
	if err := config.Application.Loads.Load(ctx, wg, call); err != nil {
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
