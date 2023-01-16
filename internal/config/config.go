package config

import (
	"github.com/rytsh/liz/loader"
	"github.com/worldline-go/turna/internal/service"
)

var Application = struct {
	LogLevel string `cfg:"log_level"`
	Loads    loader.Configs
	Services service.Services
}{
	LogLevel: "info",
}
