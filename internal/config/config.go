package config

import (
	"time"

	"github.com/rakunlabs/turna/internal/loader"
	"github.com/rakunlabs/turna/pkg/preprocess"
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

var Application = struct {
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
}{
	LogLevel: "info",
}

// Providers are dynamic configuration sources of the server.
type Providers struct {
	Kubernetes kubernetes.Config `cfg:"kubernetes"`
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
