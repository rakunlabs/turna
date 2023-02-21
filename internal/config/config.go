package config

import (
	"github.com/rytsh/liz/loader"
	"github.com/worldline-go/turna/internal/service"
	"github.com/worldline-go/turna/pkg/server"
)

var Application = struct {
	LogLevel string           `cfg:"log_level"`
	Loads    loader.Configs   `cfg:"loads"`
	Services service.Services `cfg:"services"`
	Print    string           `cfg:"print"`
	Server   server.Server    `cfg:"server"`
}{
	LogLevel: "info",
}
