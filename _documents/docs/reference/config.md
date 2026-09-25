# Config

Turna configuration is decoded into four main sections: `loads`, `preprocess`, `server`, and `services`.

```yaml
log_level: info
print: "turna started for {{ .APP_NAME }}"

loads: []
preprocess: []
server: {}
services: []
telemetry: {}
```

## Startup Order

Turna applies configuration and runtime work in this order:

1. Load bootstrap settings from environment variables.
2. Load Turna application config from Consul, Vault, file, and environment sources depending on `config_set`.
3. Run `loads` and store loaded data in template memory.
4. Run `preprocess` modules.
5. Render and write the top-level `print` message to logs when configured.
6. Start server entrypoints and routers.
7. Start services.

Dynamic `loads` can update loaded data later. When that happens, Turna refreshes template data and service filters.

## Top-Level Fields

| Field | Type | Description |
| --- | --- | --- |
| `log_level` | string | Application log level. Default is `info`. |
| `print` | string | Template rendered after loads and preprocess complete. |
| `loads` | array | External data loaders. See [Loads](./loads). |
| `preprocess` | array | Pre-start file processors. See [Preprocess](./preprocess/preprocess). |
| `server` | object | HTTP/TCP server configuration. See [Server](./server/server). |
| `services` | array | Local commands to run. See [Services](./services). |
| `telemetry` | object | OpenTelemetry exporter settings. See [Telemetry](#telemetry). |

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

## Bootstrap Settings

Bootstrap settings decide where the Turna config itself is loaded from.

```yaml
prefix:
  vault: ""
  consul: ""
app_name: turna
config_set:
  consul: false
  vault: false
  file: true
```

Equivalent environment variables:

```sh
APP_NAME=turna
PREFIX_CONSUL=finops
PREFIX_VAULT=secret
CONFIG_SET_CONSUL=false
CONFIG_SET_VAULT=false
CONFIG_SET_FILE=true
```

The effective loader order is Consul, Vault, file, and environment. Environment variables are always loaded last.

## File Config

File loading is enabled by default. Supported extensions are `toml`, `yaml`, `yml`, and `json`.

Turna looks for a file named after `APP_NAME`, which defaults to `turna`, such as `turna.yaml`. Set `CONFIG_FILE` to use a specific file name.

```sh
CONFIG_FILE=local.yaml turna
```

## Consul And Vault Client Environment

Consul uses `github.com/hashicorp/consul/api` environment variables. Common options:

| Environment variable | Description |
| --- | --- |
| `CONSUL_HTTP_ADDR` | Consul address. |
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

Vault uses `github.com/hashicorp/vault/api` environment variables. Common options:

| Environment variable | Description |
| --- | --- |
| `VAULT_ADDR` | Vault address. |
| `VAULT_TOKEN` | Vault token. |
| `VAULT_ROLE_ID` | AppRole role ID. |
| `VAULT_ROLE_SECRET` | AppRole secret. |
| `VAULT_AGENT_ADDR` | Vault agent address. |
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
| `VAULT_APPROLE_BASE_PATH` | AppRole login path. |
