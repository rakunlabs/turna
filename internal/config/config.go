package config

import (
	"time"

	"github.com/rakunlabs/turna/pkg/preprocess"
	"github.com/rakunlabs/turna/pkg/server"
	"github.com/rakunlabs/turna/pkg/service"
	"github.com/rytsh/liz/loader"
)

var (
	AppName   = "turna"
	LoadName  = ""
	StartDate = time.Now()
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

type Prefix struct {
	Vault  string `cfg:"vault"`
	Consul string `cfg:"consul"`
}

type LoadApp struct {
	Prefix    Prefix    `cfg:"prefix"`
	AppName   string    `cfg:"app_name"`
	ConfigSet GetConfig `cfg:"config_set"`
}

var LoadConfig = LoadApp{
	AppName: AppName,
	ConfigSet: GetConfig{
		Consul: false,
		Vault:  false,
		File:   true,
	},
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
