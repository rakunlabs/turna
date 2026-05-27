# Services

`services` starts local commands after loads, preprocess, and server setup. Use it to run migrations, helper processes, or the main application next to Turna.

```yaml
services:
  - name: app
    path: .
    command: ./app --config {{ .config_path }}
    env:
      APP_ENV: production
    env_values: []
    inherit_env: false
    user: ""
    filters: []
    filters_values: []
    order: 0
    depends: []
    allow_failure: false
```

## Fields

| Field | Description |
| --- | --- |
| `name` | Unique service name. Used by dependencies and logs. |
| `path` | Working directory for the command. |
| `command` | Command line. Rendered as a Turna template, then parsed like a shell command. |
| `env` | Environment variables. Values can use templates. |
| `env_values` | Paths in loaded data that provide extra environment variables. |
| `inherit_env` | Copy the current process environment before applying `env`. |
| `user` | Run as a user or uid/gid, such as `root`, `1000`, or `1000:1000`. |
| `filters` | Suppress stdout/stderr lines containing these byte strings. |
| `filters_values` | Paths in loaded data that provide additional filters. |
| `order` | Services without dependencies run by ascending order. Same order runs in parallel. |
| `depends` | Service names that must finish before this one starts. When set, `order` is ignored. |
| `allow_failure` | Continue even if the command exits with a non-zero status. |

## Dependency Example

```yaml
services:
  - name: migrate
    command: ./migrate.sh
    order: 0

  - name: cache-warmup
    command: ./warmup.sh
    order: 0
    allow_failure: true

  - name: app
    command: ./app
    depends:
      - migrate
      - cache-warmup
```

`migrate` and `cache-warmup` can run in parallel because they share the same order. `app` starts after both dependencies complete. `cache-warmup` may fail without stopping the run because `allow_failure` is true.

## Template Data

`command` and `env` values are rendered with loaded data. For example:

```yaml
loads:
  - name: app_env
    statics:
      - content:
          name: app_env
          content: |
            PORT: "3000"

services:
  - name: app
    command: ./app --port {{ .app_env.PORT }}
    env:
      PORT: "{{ .app_env.PORT }}"
```
