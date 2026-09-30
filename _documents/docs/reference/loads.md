# Loads

`loads` fetch data before preprocess, server startup, and services. Loaded data is stored in memory and can be used by templates, `server.load_value`, service environment variables, service filters, preprocessors, and response templates.

```yaml
loads:
  - name: app_config
    export: app_config.yaml
    file_perm: "0644"
    folder_perm: "0755"
    statics: []
    dynamics: []
```

| Field | Description |
| --- | --- |
| `name` | Key used to expose loaded data in memory. |
| `export` | Optional output file path. Omit it to keep data in memory only. |
| `file_perm` | Permission used for exported files. |
| `folder_perm` | Permission used for created export directories. |
| `statics` | Sources loaded once at startup. |
| `dynamics` | Sources watched or reloaded by the loader implementation. |

Turna implements the loader in `internal/loader` and then consumes the resulting data through `render.Data`. Sources that set `template: true` are rendered with Turna's mugo engine, the same one used by `print`, service env/command, filters, and server config.

## Static Sources

Static sources are loaded once at startup. Supported source types are `consul`, `vault`, `file`, `http`, `content`, `aws_secrets`, `aws_ssm`, `azure_key_vault`, `gcp_secret`, and `gcp_parameter`.

Every source shares these fields:

| Field | Description |
| --- | --- |
| `name` | Key used to expose this source's value in memory. |
| `raw` | Keep the content as raw bytes instead of decoding it. |
| `codec` | `YAML`, `JSON` or `TOML`. Default is YAML; `file` detects it from the extension and `http` from `Content-Type`. |
| `inner_path` | `/` separated path to pick from the decoded value. |
| `map` | `/` separated path to wrap the value in. |
| `template` | Render the content as a template before decoding. |
| `base64` | Base64 decode the resulting raw value. |

### Consul

```yaml
loads:
  - name: app
    statics:
      - consul:
          name: consul_app
          path: app/config
          path_prefix: finops
          codec: YAML
          raw: false
          inner_path: server
          map: app/server
          template: false
          base64: false
```

### Vault

Reads a KVv2 secret with chu's vault loader. `VAULT_ADDR` (or
`VAULT_AGENT_ADDR`) is required. AppRole login is used when `VAULT_ROLE_ID`
(and `VAULT_ROLE_SECRET`) is set, otherwise the client uses `VAULT_TOKEN`.
`path_prefix` falls back to `VAULT_SECRET_BASE_PATH` and `app_role_base_path`
to `VAULT_APPROLE_BASE_PATH`. The secret data is passed as JSON, so `raw: true`
gives the JSON document.

```yaml
loads:
  - name: secret
    statics:
      - vault:
          name: vault_app
          path: app
          path_prefix: secret
          app_role_base_path: auth/approle/login
          inner_path: data
          map: app/secret
          template: false
          base64: false
```

### File

```yaml
loads:
  - name: local
    statics:
      - file:
          name: local_file
          path: config/app.yaml
          codec: YAML
          raw: false
          inner_path: server
          map: app/server
          template: false
          base64: false
```

### HTTP

Fetches configuration from an HTTP endpoint. When `codec` is empty, the codec is
detected from the response `Content-Type` header (`application/json`,
`application/x-toml`, `application/yaml`, ...) and falls back to YAML.

```yaml
loads:
  - name: remote
    statics:
      - http:
          name: remote_http
          url: https://config.example.com/app
          method: GET
          headers:
            Authorization: Bearer token
          query:
            version: v1
          body: ""
          timeout: 5s
          codec: ""
          insecure_skip_verify: false
          raw: false
          inner_path: server
          map: app/server
          template: false
          base64: false
```

### Content

```yaml
loads:
  - name: app_config
    statics:
      - content:
          name: app_config
          codec: YAML
          content: |
            entrypoints:
              web:
                address: ":8080"
          raw: false
          template: false
          inner_path: ""
          map: ""
          base64: false
```

## Cloud Sources

Cloud sources use the same clients as chu's external loaders and the default
credential chain of each provider (AWS default config, Azure
`DefaultAzureCredential`, GCP Application Default Credentials).

Target fields are optional when the related environment variable is set, the
same variables chu uses. A value in the config overrides the environment.

| Source | Field | Environment fallback |
| --- | --- | --- |
| `aws_secrets` | `region` | `AWS_REGION`, `AWS_DEFAULT_REGION` |
| `aws_secrets` | `prefix` | `AWS_SECRETS_PREFIX` |
| `aws_ssm` | `region` | `AWS_REGION`, `AWS_DEFAULT_REGION` |
| `aws_ssm` | `prefix` | `AWS_SSM_PREFIX` |
| `azure_key_vault` | `vault_url` | `AZURE_KEYVAULT_URL`, `AZURE_KEYVAULT_NAME` (`https://<name>.vault.azure.net/`) |
| `gcp_secret` | `project_id` | `GCP_PROJECT_ID` |
| `gcp_secret` | `location` | `GCP_SECRET_LOCATION` (default `global`) |
| `gcp_secret` | `version` | `GCP_SECRET_VERSION` (default `latest`) |
| `gcp_parameter` | `project_id` | `GCP_PROJECT_ID` |
| `gcp_parameter` | `location` | `GCP_PARAMETER_LOCATION` (default `global`) |
| `gcp_parameter` | `version` | `GCP_PARAMETER_VERSION` (default `latest`) |

```yaml
loads:
  - name: app
    export: app.yaml
    statics:
      # AWS Secrets Manager, secret id becomes <prefix>/<secret_id>
      - aws_secrets:
          secret_id: app/config
          region: eu-west-1
      # AWS SSM Parameter Store, loaded with decryption as /<prefix>/<parameter>
      - aws_ssm:
          parameter: app/config
          codec: JSON
      # Azure Key Vault, "/" in secret is replaced with "-"
      - azure_key_vault:
          secret: app-config
          vault_url: https://myvault.vault.azure.net/
      # GCP Secret Manager, "/" in secret is replaced with "-"
      - gcp_secret:
          secret: app
          inner_path: database
          map: db
      # GCP Parameter Manager, rendered payload is used
      - gcp_parameter:
          parameter: app
          project_id: my-project
          location: europe-west1
```

## Dynamic Sources

Dynamic sources are reloaded by the loader implementation. Turna updates in-memory data and service filters when dynamic data changes.

```yaml
loads:
  - name: dynamic_config
    dynamics:
      - consul:
          name: dynamic_consul
          path: app/dynamic
          path_prefix: finops
          codec: YAML
          raw: false
          inner_path: ""
          map: ""
          template: false
```

## Using Loaded Data As Server Config

`server.load_value` replaces the server configuration with a loaded data key after `loads` complete.

```yaml
loads:
  - name: server
    statics:
      - content:
          codec: YAML
          content: |
            entrypoints:
              web:
                address: ":8080"

server:
  load_value: server
  http:
    middlewares: {}
    routers: {}
```
