package loader

import "time"

// Configs is a list of load configurations.
type Configs []Config

// Config is a single load configuration.
type Config struct {
	// Name for export value, default is empty.
	Name       string          `cfg:"name"`
	Export     string          `cfg:"export"`
	FilePerm   string          `cfg:"file_perm"`
	FolderPerm string          `cfg:"folder_perm"`
	Statics    []ConfigStatic  `cfg:"statics"`
	Dynamics   []ConfigDynamic `cfg:"dynamics"`
}

// ConfigStatic is a source loaded once at startup.
type ConfigStatic struct {
	Consul  *ConfigConsul  `cfg:"consul"`
	Vault   *ConfigVault   `cfg:"vault"`
	File    *ConfigFile    `cfg:"file"`
	Content *ConfigContent `cfg:"content"`
	HTTP    *ConfigHTTP    `cfg:"http"`
}

// ConfigDynamic is a source watched/reloaded while running.
type ConfigDynamic struct {
	Consul *ConfigConsul `cfg:"consul"`
}

type ConfigConsul struct {
	// Name for export, default is empty.
	Name string `cfg:"name"`
	// Path is the location in consul KV.
	Path string `cfg:"path"`
	// PathPrefix default is empty.
	PathPrefix string `cfg:"path_prefix"`
	// Raw to load as raw, don't mix with other loaders.
	Raw bool `cfg:"raw"`
	// Codec YAML,JSON,TOML default is YAML.
	Codec string `cfg:"codec"`
	// InnerPath is get the inner path from response, / separated as db/settings.
	// Cannot work with Raw.
	InnerPath string `cfg:"inner_path"`
	// Map is the wrapper map, / separated as db/settings.
	Map string `cfg:"map"`
	// Template to run go template after the load.
	Template bool `cfg:"template"`
	// Base64 to decode the content.
	Base64 bool `cfg:"base64"`
}

type ConfigVault struct {
	// Name for export, default is empty.
	Name string `cfg:"name"`
	Path string `cfg:"path"`
	// PathPrefix default is empty, path_prefix is must!
	PathPrefix string `cfg:"path_prefix"`
	// AppRoleBasePath default is auth/approle/login, not need to set.
	AppRoleBasePath string `cfg:"app_role_base_path"`
	// InnerPath is get the inner path from vault response, / separated as db/settings.
	InnerPath string `cfg:"inner_path"`
	// Map is the wrapper map, / separated as db/settings.
	Map string `cfg:"map"`
	// Template to run go template after the load.
	Template bool `cfg:"template"`
	// Base64 to decode the content.
	Base64 bool `cfg:"base64"`
}

type ConfigFile struct {
	// Name for export, default is empty.
	Name string `cfg:"name"`
	// Path is the file location, [toml, yml, yaml, json] supported.
	Path string `cfg:"path"`
	// Raw to load as raw, don't mix with other loaders.
	Raw bool `cfg:"raw"`
	// InnerPath is get the inner path from response, / separated as db/settings.
	// Cannot work with Raw.
	InnerPath string `cfg:"inner_path"`
	// Map is the wrapper map, / separated as db/settings.
	Map string `cfg:"map"`
	// Template to run go template after the load.
	Template bool `cfg:"template"`
	// Base64 to decode the content.
	Base64 bool `cfg:"base64"`
}

type ConfigHTTP struct {
	// Name for export, default is empty.
	Name string `cfg:"name"`
	// URL is the endpoint to fetch the configuration from.
	URL string `cfg:"url"`
	// Method is the HTTP method, default is GET.
	Method string `cfg:"method"`
	// Headers to set on the request.
	Headers map[string]string `cfg:"headers"`
	// Query parameters added to the URL.
	Query map[string]string `cfg:"query"`
	// Body is the optional request body.
	Body string `cfg:"body"`
	// Timeout for the request, default is no timeout.
	Timeout time.Duration `cfg:"timeout"`
	// Codec YAML,JSON,TOML default detects from the response Content-Type
	// and falls back to YAML.
	Codec string `cfg:"codec"`
	// InsecureSkipVerify disables TLS certificate verification.
	InsecureSkipVerify bool `cfg:"insecure_skip_verify"`
	// Raw to load as raw, don't mix with other loaders.
	Raw bool `cfg:"raw"`
	// InnerPath is get the inner path from response, / separated as db/settings.
	// Cannot work with Raw.
	InnerPath string `cfg:"inner_path"`
	// Map is the wrapper map, / separated as db/settings.
	Map string `cfg:"map"`
	// Template to run go template after the load.
	Template bool `cfg:"template"`
	// Base64 to decode the content.
	Base64 bool `cfg:"base64"`
}

type ConfigContent struct {
	// Name for export, default is empty.
	Name string `cfg:"name"`
	// Codec YAML,JSON,TOML default is YAML.
	Codec   string `cfg:"codec"`
	Content string `cfg:"content"`
	Raw     bool   `cfg:"raw"`
	// InnerPath is get the inner path from response, / separated as db/settings.
	// Cannot work with Raw.
	InnerPath string `cfg:"inner_path"`
	// Map is the wrapper map, / separated as db/settings.
	Map string `cfg:"map"`
	// Template to run go template after the load.
	Template bool `cfg:"template"`
	// Base64 to decode the content.
	Base64 bool `cfg:"base64"`
}
