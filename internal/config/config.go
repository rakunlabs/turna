package config

import (
	"github.com/rytsh/liz/loader"
	"github.com/worldline-go/turna/pkg/preprocess"
	"github.com/worldline-go/turna/pkg/server"
	"github.com/worldline-go/turna/pkg/service"
)

var Application = struct {
	LogLevel   string             `cfg:"log_level"`
	Loads      loader.Configs     `cfg:"loads"`
	Services   service.Services   `cfg:"services"`
	Print      string             `cfg:"print"`
	Server     server.Server      `cfg:"server"`
	Preprocess preprocess.Configs `cfg:"preprocess"`
}{
	LogLevel: "info",
}
