# Config

Turna configuration is decoded into four main sections: `loads`, `preprocess`, `server`, and `services`.

```yaml
log_level: info
print: "turna started for {{ .APP_NAME }}"

loads: []
env: {}
env_values: []
preprocess: []
server: {}
services: []
telemetry: {}
```

## Startup Order

Turna applies configuration and runtime work in this order:

1. Load bootstrap settings from environment variables.
2. Load Turna application config from the [application config sources](#application-config-sources): Consul, AWS, Azure, Vault, GCP, HTTP, file, and environment.
3. Run `loads` and store loaded data in template memory.
4. Apply top-level `env_values` and `env` to Turna's own process environment, then refresh the default HTTP transport's proxy settings.
5. Run `preprocess` modules.
6. Render and write the top-level `print` message to logs when configured.
7. Initialize telemetry and start server entrypoints and routers.
8. Start services.

Dynamic `loads` can update loaded data later. When that happens, Turna refreshes template data and service filters.

## Top-Level Fields

| Field | Type | Description |
| --- | --- | --- |
| `log_level` | string | Application log level. Default is `info`. |
| `print` | string | Template rendered after loads and preprocess complete. |
| `loads` | array | External data loaders. See [Loads](./loads). |
| `env` | object | Set or overwrite Turna's process environment after loads. Values support templates. |
| `env_values` | array | `/`-separated paths to maps in loaded data to export as process environment. Later paths override earlier ones; `env` has final precedence. Missing or non-map paths fail startup. |
| `preprocess` | array | Pre-start file processors. See [Preprocess](./preprocess/preprocess). |
| `server` | object | HTTP/TCP server configuration. See [Server](./server/server). |
| `services` | array | Local commands to run. See [Services](./services). |
| `telemetry` | object | OpenTelemetry exporter settings. See [Telemetry](#telemetry). |

The debug `loaded config` log respects `log:"-"` struct tags: tagged fields are omitted, while other fields keep their `cfg` names. This does not affect configuration decoding. Process/service environment maps, inline loader content, credentials, private keys and marked authentication fields are excluded. This is not a general string scrubber: avoid putting secrets in unmarked fields such as commands or URLs, or in explicit `print` templates.

## Process Environment

```yaml
loads:
  - name: settings
    statics:
      - content:
          content: |
            environment:
              OTEL_SERVICE_NAME: turna-proxy
env_values:
  - settings/environment
env:
  HTTPS_PROXY: http://proxy.internal:8080
```

These settings add or overwrite variables in Turna itself, not just child services. Unspecified variables are preserved; an empty value sets an empty variable rather than removing it. Values are rendered and validated before environment changes are applied. Templates see the environment from before this step, so do not rely on one entry referencing another via `env`.

This runs once at startup. Dynamic loads do not reapply environment settings. It does not re-decode already-loaded application config, reinitialize clients, or change the parent shell. Child services inherit these variables according to their `inherit_env` setting and can override them with their own `env`.

Only components that read the environment after this step observe the new values. In particular, `gcp_token` reads credentials, certificates and audience from its middleware config, not automatically from environment variables; setting a GCP environment variable does not replace those fields or invalidate cached tokens.

After applying environment settings, Turna replaces the default HTTP transport with a clone whose proxy function reads a fresh snapshot of `HTTP_PROXY`, `HTTPS_PROXY` and `NO_PROXY` (and their lowercase equivalents). This bypasses Go's process-wide proxy environment cache, even if loads already made HTTP requests. Middleware clients that subsequently use or clone the default transport, including `gcp_token`, use the new proxy settings. Existing clients with their own transports and clients with explicit proxy settings are unaffected. The proxy snapshot is not refreshed on dynamic reload.

## Telemetry

`telemetry` configures the global OpenTelemetry meter and tracer providers with [`github.com/rakunlabs/tell`](https://github.com/rakunlabs/tell). Metrics and traces are exported over OTLP/gRPC. Without a collector address (and without `OTEL_EXPORTER_OTLP_ENDPOINT`) the providers are noop, so the telemetry middlewares cost almost nothing.

```yaml
telemetry:
  collector: otel-collector:4317
  server_name: ""
  tls:
    enabled: false
    insecure_skip_verify: false
    cert_file: ""
    key_file: ""
    ca_file: ""
  metric:
    disabled: false
    default:
      go_runtime: true
  trace:
    disabled: false
```

Telemetry is recorded by the `telemetry` middlewares of [HTTP](./server/http/middlewares/telemetry), [TCP](./server/tcp/middlewares/telemetry) and [UDP](./server/udp/middlewares/telemetry). Service name and resource attributes follow the standard `OTEL_SERVICE_NAME` and `OTEL_RESOURCE_ATTRIBUTES` environment variables.

## Application Config Sources

Turna's own configuration (everything on this page) is loaded with [`github.com/rakunlabs/chu`](https://github.com/rakunlabs/chu). Every source looks up a document named after the application, `turna` by default, and later sources override earlier ones:

| Order | Source | Enabled when | Document looked up |
| --- | --- | --- | --- |
| 1 | `default` | always | `default` struct tags |
| 2 | `consul` | `CONSUL_HTTP_ADDR` is set | KV key `<CONSUL_CONFIG_PATH_PREFIX>/turna` |
| 3 | `awsssm` | `AWS_REGION` or `AWS_DEFAULT_REGION` is set | parameter `/<AWS_SSM_PREFIX>/turna` |
| 4 | `awssecrets` | `AWS_REGION` or `AWS_DEFAULT_REGION` is set | secret `<AWS_SECRETS_PREFIX>/turna` |
| 5 | `azurekeyvault` | `AZURE_KEYVAULT_URL` or `AZURE_KEYVAULT_NAME` is set | secret `turna` |
| 6 | `vault` | `VAULT_ADDR` or `VAULT_AGENT_ADDR` is set | KVv2 `<VAULT_SECRET_BASE_PATH>/turna` |
| 7 | `gcpparameter` | `GCP_PROJECT_ID` is set | parameter `turna` |
| 8 | `gcpsecret` | `GCP_PROJECT_ID` is set | secret `turna` |
| 9 | `http` | `CONFIG_HTTP_ADDR` is set | `GET <CONFIG_HTTP_ADDR>/turna<CONFIG_HTTP_SUFFIX>` |
| 10 | `file` | always | `turna.toml`, `.yaml`, `.yml` or `.json` |
| 11 | `env` | always | environment variables and `.env` files |

A source whose address variable is missing, or whose document does not exist, is skipped. Any other error, such as failed authentication, stops startup.

Remote documents are decoded as YAML, so JSON works as well.

```sh
# Base config in Consul, secrets from GCP Secret Manager, overrides from env
CONSUL_HTTP_ADDR=consul:8500 \
CONSUL_CONFIG_PATH_PREFIX=finops \
GCP_PROJECT_ID=my-project \
LOG_LEVEL=debug \
turna
```

### Naming

| Environment variable | Description |
| --- | --- |
| `APP_NAME` | Document name used by every source. Default is `turna`. |
| `CONFIG_NAME_PREFIX` | Prepended to the name as `<prefix>/<APP_NAME>`. |
| `CONFIG_SET_<SOURCE>` | Set to `false` to skip a source, for example `CONFIG_SET_VAULT=false` or `CONFIG_SET_GCPSECRET=false`. Useful when a backend is down and should not block startup. |

### File

Turna looks for `turna.toml`, `turna.yaml`, `turna.yml` or `turna.json` in the current directory, then in `/etc`. `CONFIG_FILE_TURNA` (the uppercased app name) or `CONFIG_FILE` points to a specific file instead.

```sh
CONFIG_FILE=local.yaml turna
```

### Environment

Environment variables are loaded last and override every other source. Field paths are uppercased and joined with `_`, so `log_level` is `LOG_LEVEL` and `telemetry.collector` is `TELEMETRY_COLLECTOR`. `.env` and `.env.local` in the current directory are read automatically; `CONFIG_ENV_FILE` adds one more file.

### HTTP

| Environment variable | Description |
| --- | --- |
| `CONFIG_HTTP_ADDR` | Base URL. The app name is appended as a path. |
| `CONFIG_HTTP_SUFFIX` | Appended after the name, for example `.yaml`. |
| `CONFIG_HTTP_QUERY` | Query string added to the request, for example `env=prod`. |

Only a `200` response is used; `204` and `404` skip the source.

### Consul

| Environment variable | Description |
| --- | --- |
| `CONSUL_HTTP_ADDR` | Consul address. Required to enable the source. |
| `CONSUL_CONFIG_PATH_PREFIX` | Key prefix, the key becomes `<prefix>/turna`. |

The client also reads the standard `github.com/hashicorp/consul/api` variables:

| Environment variable | Description |
| --- | --- |
| `CONSUL_HTTP_TOKEN_FILE` | File containing the Consul token. |
| `CONSUL_HTTP_TOKEN` | Consul token. |
| `CONSUL_HTTP_AUTH` | HTTP basic auth. |
| `CONSUL_HTTP_SSL` | Enable TLS. |
| `CONSUL_TLS_SERVER_NAME` | TLS server name. |
| `CONSUL_CACERT` | CA certificate file. |
| `CONSUL_CAPATH` | CA certificate directory. |
| `CONSUL_CLIENT_CERT` | Client certificate. |
| `CONSUL_CLIENT_KEY` | Client key. |
| `CONSUL_HTTP_SSL_VERIFY` | TLS verification toggle. |
| `CONSUL_NAMESPACE` | Consul namespace. |
| `CONSUL_PARTITION` | Consul partition. |

### Vault

| Environment variable | Description |
| --- | --- |
| `VAULT_ADDR`, `VAULT_AGENT_ADDR` | Vault address. Required to enable the source. |
| `VAULT_SECRET_BASE_PATH` | KVv2 mount path. Required when Vault is enabled. |
| `VAULT_ROLE_ID` | AppRole role ID. When set, Turna logs in with AppRole; otherwise `VAULT_TOKEN` is used. |
| `VAULT_ROLE_SECRET` | AppRole secret ID. |
| `VAULT_APPROLE_BASE_PATH` | AppRole login path. Default is `auth/approle/login`. |

The client also reads the standard `github.com/hashicorp/vault/api` variables:

| Environment variable | Description |
| --- | --- |
| `VAULT_TOKEN` | Vault token. |
| `VAULT_MAX_RETRIES` | Retry count. |
| `VAULT_CACERT` | CA certificate file. |
| `VAULT_CACERT_BYTES` | CA certificate bytes. |
| `VAULT_CAPATH` | CA certificate directory. |
| `VAULT_CLIENT_CERT` | Client certificate. |
| `VAULT_CLIENT_KEY` | Client key. |
| `VAULT_RATE_LIMIT` | Client-side rate limit. |
| `VAULT_CLIENT_TIMEOUT` | Client timeout. |
| `VAULT_SKIP_VERIFY` | Skip TLS verification. |
| `VAULT_TLS_SERVER_NAME` | TLS server name. |
| `VAULT_HTTP_PROXY` | HTTP proxy. |
| `VAULT_PROXY_ADDR` | Proxy address. |
| `VAULT_DISABLE_REDIRECTS` | Disable redirects. |

### AWS

Both AWS sources use the default AWS credential chain (environment, shared config, IAM role).

| Environment variable | Description |
| --- | --- |
| `AWS_REGION`, `AWS_DEFAULT_REGION` | Region. Required to enable the sources. |
| `AWS_SSM_PREFIX` | Parameter Store prefix, the name becomes `/<prefix>/turna`. Loaded with decryption. |
| `AWS_SECRETS_PREFIX` | Secrets Manager prefix, the secret id becomes `<prefix>/turna`. |

### Azure Key Vault

Uses `DefaultAzureCredential` (environment, managed identity, Azure CLI). The latest version of the secret is loaded; `/` in the name is replaced with `-`.

| Environment variable | Description |
| --- | --- |
| `AZURE_KEYVAULT_URL` | Vault URL, for example `https://my-vault.vault.azure.net/`. |
| `AZURE_KEYVAULT_NAME` | Vault name, used to build the URL when `AZURE_KEYVAULT_URL` is unset. |

### GCP

Both GCP sources use Application Default Credentials (`GOOGLE_APPLICATION_CREDENTIALS`, workload identity). `/` in the name is replaced with `-`.

| Environment variable | Description |
| --- | --- |
| `GCP_PROJECT_ID` | Project id. Required to enable the sources. |
| `GCP_PARAMETER_LOCATION` | Parameter Manager location. Default is `global`. |
| `GCP_PARAMETER_VERSION` | Parameter version to render. Default is `latest`. Referenced secrets are resolved server-side. |
| `GCP_PARAMETER_FALLBACK` | When `true` and `latest` is disabled, use the most recent enabled version. |
| `GCP_SECRET_LOCATION` | Secret Manager location. Default is `global`. |
| `GCP_SECRET_VERSION` | Secret version. Default is `latest`. |
| `GCP_SECRET_ADDITIONAL` | Comma-separated shared secrets loaded before `turna`, for example `generic,keycloak`. Missing ones are skipped. |
