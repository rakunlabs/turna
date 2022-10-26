package config

import (
	"github.com/worldline-go/turna/internal/load"
	"github.com/worldline-go/turna/internal/service"
)

var Application = struct {
	LogLevel string `cfg:"log_level"`
	Loads    load.Loads
	Services service.Services
}{
	LogLevel: "info",
}
