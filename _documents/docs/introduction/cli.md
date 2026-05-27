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
| `--config-consul` | `false` | Enable Consul as a Turna config source. |
| `--config-vault` | `false` | Enable Vault as a Turna config source. Requires `PREFIX_VAULT`. |
| `--config-file` | `true` | Enable file-based config loading. |
| `-v`, `--version` | | Print version metadata. |

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
| `-s`, `--slient` | `false` | Suppress response body output. The flag name is currently spelled `slient` in the CLI. |
| `--ping` | `false` | Deprecated compatibility flag. |

## Shell Completion

Cobra provides completion subcommands through `turna completion`. Run `turna completion --help` for the supported shells.
