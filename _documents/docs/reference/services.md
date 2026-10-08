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
    stop_timeout: 30s
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
| `inherit_env` | Copy the full current process environment before applying `env`. Even when false, a minimal baseline (`PATH`, `HOME`, `TMPDIR`, `USER`, `LOGNAME`, `SHELL`, `LANG`, `TZ` and Windows equivalents) is always inherited; `env` can override any of them. |
| `user` | Run as a user or uid/gid, such as `root`, `1000`, or `1000:1000`. Single value uses that user's primary group. |
| `tty` | Attach Turna's terminal to a service-owned PTY (default `false`, Linux/macOS only). Requires terminal stdin/stdout; only one service may enable it. Cannot be combined with filters. |
| `stop_timeout` | Wait duration after SIGTERM before force killing the process on shutdown. Default `30s`. |
| `filters` | Suppress stdout/stderr lines containing these byte strings. |
| `filters_values` | Paths in loaded data that provide additional filters. |
| `order` | Services without dependencies run by ascending order. Same order runs in parallel. |
| `depends` | Service names that must finish before this one starts. When set, `order` is ignored. |
| `allow_failure` | Continue even if the command exits with a non-zero status. |

## Interactive Terminal

```yaml
services:
  - name: shell
    command: /bin/bash -i
    inherit_env: true
    tty: true
```

Run Turna in a foreground terminal (for containers, allocate stdin and a TTY,
for example with `docker run -it`). Keyboard input, terminal resizing, and
control keys such as Ctrl+C and Ctrl+D are forwarded to the service. Ctrl+C
interrupts the shell's foreground command, not Turna. Terminal settings are
restored when the service exits or Turna shuts down normally.

TTY output bypasses line filtering and merges stdout/stderr. Other services
and Turna logs can still appear on the same screen; redirect their logs if
you need a clean shell display. Other services must not read shared stdin
while the interactive service is running. Windows and non-terminal input
(such as pipes or background execution without a terminal) are rejected.

Without `tty`, services inherit Turna's stdin but do not receive foreground
terminal control; piped input works, but this is not an interactive shell mode.

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
