package config

import (
	"time"

	"github.com/rakunlabs/turna/internal/loader"
	"github.com/rakunlabs/turna/pkg/preprocess"
	"github.com/rakunlabs/turna/pkg/provider/docker"
	"github.com/rakunlabs/turna/pkg/provider/kubernetes"
	"github.com/rakunlabs/turna/pkg/server"
	"github.com/rakunlabs/turna/pkg/service"
	"github.com/rakunlabs/turna/pkg/telemetry"
)

var (
	AppName   = "turna"
	LoadName  = ""
	StartDate = time.Now()
)

// App is the application configuration.
type App struct {
	LogLevel   string             `cfg:"log_level"`
	Loads      loader.Configs     `cfg:"loads"`
	Env        map[string]any     `cfg:"env" log:"-"`
	EnvValues  []string           `cfg:"env_values"`
	Services   service.Services   `cfg:"services"`
	Print      string             `cfg:"print"`
	Server     server.Server      `cfg:"server"`
	Preprocess preprocess.Configs `cfg:"preprocess"`
	Telemetry  telemetry.Config   `cfg:"telemetry"`
	Providers  Providers          `cfg:"providers"`
	Reload     Reload             `cfg:"reload"`
}

// NewApp returns the configuration with its defaults.
func NewApp() App {
	return App{LogLevel: "info"}
}

var Application = NewApp()

// Reload applies a changed configuration file to the running server.
// Only the server section is replaced; loads, services, telemetry and
// providers keep the configuration they started with.
type Reload struct {
	// Signal reloads on SIGHUP, default true.
	Signal *bool `cfg:"signal"`
	// Watch reloads when the configuration file changes.
	Watch bool `cfg:"watch"`
	// Debounce waits for writes to settle before reloading, default 500ms.
	Debounce time.Duration `cfg:"debounce"`
	// Interval reloads periodically, e.g. to pick up changes of remote
	// sources such as Consul or Vault; 0 disables it.
	Interval time.Duration `cfg:"interval"`
}

// Providers are dynamic configuration sources of the server.
type Providers struct {
	Kubernetes kubernetes.Config `cfg:"kubernetes"`
	Docker     docker.Config     `cfg:"docker"`
}

type GetConfig struct {
	Vault  bool
	Consul bool
	File   bool
}

type Build struct {
	Version string
	Commit  string
	Date    string
}

var BuildVars = Build{}
