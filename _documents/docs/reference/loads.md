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

Static sources are loaded once at startup. Supported source types are `consul`, `vault`, `file`, and `content`.

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
