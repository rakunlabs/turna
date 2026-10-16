# Command Line Interface

Running `turna` without a subcommand loads configuration and starts configured loads, preprocessors, servers, and services. If no configuration is found, Turna starts with the defaults and has nothing to run.

## Root Command

```sh
turna [flags]
```

Flags:

| Flag | Default | Description |
| --- | --- | --- |
| `-l`, `--log-level` | `info` | Log level. Command-line value overrides loaded config. |
| `-v`, `--version` | | Print version metadata. |

Config sources are selected with environment variables, not flags. See [Application Config Sources](../reference/config#application-config-sources).

## API Command

```sh
turna api --url http://localhost:8080/health --method GET
```

The `api` command is a small HTTP caller. It exits with an error when a response status is outside the `2xx` range.

Flags:

| Flag | Default | Description |
| --- | --- | --- |
| `-u`, `--url` | | URL to call. Can be provided more than once. |
| `-m`, `--method` | `GET` | HTTP method. |
| `-k`, `--insecure` | `false` | Skip TLS certificate verification. |
| `-s`, `--silent` | `false` | Suppress response body output. |

## Shell Completion

Cobra provides completion subcommands through `turna completion`. Run `turna completion --help` for the supported shells.
