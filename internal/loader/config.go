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
	Consul        *ConfigConsul        `cfg:"consul"`
	Vault         *ConfigVault         `cfg:"vault"`
	File          *ConfigFile          `cfg:"file"`
	Content       *ConfigContent       `cfg:"content"`
	HTTP          *ConfigHTTP          `cfg:"http"`
	AWSSecrets    *ConfigAWSSecrets    `cfg:"aws_secrets"`
	AWSSSM        *ConfigAWSSSM        `cfg:"aws_ssm"`
	AzureKeyVault *ConfigAzureKeyVault `cfg:"azure_key_vault"`
	GCPSecret     *ConfigGCPSecret     `cfg:"gcp_secret"`
	GCPParameter  *ConfigGCPParameter  `cfg:"gcp_parameter"`
}

// ConfigDynamic is a source watched/reloaded while running.
type ConfigDynamic struct {
	Consul *ConfigConsul `cfg:"consul"`
}

// ConfigCommon holds the post-processing options shared by all sources.
type ConfigCommon struct {
	// Name for export, default is empty.
	Name string `cfg:"name"`
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

type ConfigConsul struct {
	ConfigCommon `cfg:",inline"`
	// Path is the location in consul KV.
	Path string `cfg:"path"`
	// PathPrefix default is empty.
	PathPrefix string `cfg:"path_prefix"`
}

type ConfigVault struct {
	// Codec defaults to JSON, the KV data is passed as JSON.
	ConfigCommon `cfg:",inline"`
	// Path is the secret key under the KVv2 mount.
	Path string `cfg:"path"`
	// PathPrefix is the KVv2 mount path.
	//  - Falls back to VAULT_SECRET_BASE_PATH env.
	PathPrefix string `cfg:"path_prefix"`
	// AppRoleBasePath default is auth/approle/login, not need to set.
	//  - Falls back to VAULT_APPROLE_BASE_PATH env.
	//  - AppRole login is used only when VAULT_ROLE_ID is set, otherwise VAULT_TOKEN.
	AppRoleBasePath string `cfg:"app_role_base_path"`
}

type ConfigFile struct {
	// Codec defaults to the file extension [toml, yml, yaml, json].
	ConfigCommon `cfg:",inline"`
	// Path is the file location.
	Path string `cfg:"path"`
}

type ConfigHTTP struct {
	// Codec defaults to the response Content-Type and falls back to YAML.
	ConfigCommon `cfg:",inline"`
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
	// InsecureSkipVerify disables TLS certificate verification.
	InsecureSkipVerify bool `cfg:"insecure_skip_verify"`
}

type ConfigContent struct {
	ConfigCommon `cfg:",inline"`
	Content      string `cfg:"content"`
}

type ConfigAWSSecrets struct {
	ConfigCommon `cfg:",inline"`
	// SecretID is the secret name or ARN.
	SecretID string `cfg:"secret_id"`
	// Prefix is prepended as prefix/secret_id, ignored for ARNs.
	//  - Falls back to AWS_SECRETS_PREFIX env.
	Prefix string `cfg:"prefix"`
	// Region falls back to AWS_REGION, AWS_DEFAULT_REGION env.
	Region string `cfg:"region"`
}

type ConfigAWSSSM struct {
	ConfigCommon `cfg:",inline"`
	// Parameter is the SSM parameter name, loaded with decryption.
	Parameter string `cfg:"parameter"`
	// Prefix is prepended as /prefix/parameter.
	//  - Falls back to AWS_SSM_PREFIX env.
	Prefix string `cfg:"prefix"`
	// Region falls back to AWS_REGION, AWS_DEFAULT_REGION env.
	Region string `cfg:"region"`
}

type ConfigAzureKeyVault struct {
	ConfigCommon `cfg:",inline"`
	// Secret is the secret name, "/" is replaced with "-". Latest version is loaded.
	Secret string `cfg:"secret"`
	// VaultURL like https://myvault.vault.azure.net/
	//  - Falls back to AZURE_KEYVAULT_URL env, then AZURE_KEYVAULT_NAME env.
	VaultURL string `cfg:"vault_url"`
}

type ConfigGCPSecret struct {
	ConfigCommon `cfg:",inline"`
	// Secret is the secret id, "/" is replaced with "-".
	Secret string `cfg:"secret"`
	// ProjectID falls back to GCP_PROJECT_ID env.
	ProjectID string `cfg:"project_id"`
	// Location falls back to GCP_SECRET_LOCATION env, default is global.
	Location string `cfg:"location"`
	// Version falls back to GCP_SECRET_VERSION env, default is latest.
	Version string `cfg:"version"`
}

type ConfigGCPParameter struct {
	ConfigCommon `cfg:",inline"`
	// Parameter is the parameter id, "/" is replaced with "-".
	Parameter string `cfg:"parameter"`
	// ProjectID falls back to GCP_PROJECT_ID env.
	ProjectID string `cfg:"project_id"`
	// Location falls back to GCP_PARAMETER_LOCATION env, default is global.
	Location string `cfg:"location"`
	// Version falls back to GCP_PARAMETER_VERSION env, default is latest.
	Version string `cfg:"version"`
}
