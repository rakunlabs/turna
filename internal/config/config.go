package config

import (
	"github.com/rakunlabs/turna/pkg/preprocess"
	"github.com/rakunlabs/turna/pkg/server"
	"github.com/rakunlabs/turna/pkg/service"
	"github.com/rytsh/liz/loader"
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
