# Examples

Complete, runnable configurations for common Turna setups. Most examples are a single `turna.yaml`; Turna reads it from the current directory, or from the file in `CONFIG_FILE`:

```sh
turna
CONFIG_FILE=examples/auth.yaml turna
```

In containers the configuration usually comes from Consul or Vault instead, see [Wrap an Application](/examples/wrap#config-from-consul-or-vault).

## Serving and routing

| Example | Shows |
| --- | --- |
| [Reverse Proxy](/examples/reverse_proxy) | API gateway with `service`, `strip_prefix`, `cors`, `gzip`, `rate_limit`, and `access_log`. |
| [File Server](/examples/folder) | Static files and SPA serving with the `folder` middleware. |
| [TLS](/examples/tls) | HTTPS entrypoints with self-signed certificates or automatic ACME (Let's Encrypt). |
| [View](/examples/view) | Swagger documentation UI with the `view` middleware. |
| [Single Page App](/examples/spa) | A frontend image served by Turna with a shared login, permission checks and a custom sign-in page. |

## Authentication

| Example | Shows |
| --- | --- |
| [Auth](/examples/auth) | An identity service with the PostgreSQL-backed `auth` middleware, `session`, `login`, logout and a permission endpoint for other applications. |
| [Basic Auth](/examples/basic_auth) | Protecting paths with htpasswd-style `basic_auth`. |
| [Login](/examples/login) | Browser sessions against an external OAuth2 provider with `session`, `login`, and `role`. |
| [Kafka OAUTHBEARER With GCP](/examples/gcp_kafka) | `gcp_token` as a local token endpoint for Redpanda Console using X.509 workload identity federation. |

## Configuration and processes

| Example | Shows |
| --- | --- |
| [Wrap an Application](/examples/wrap) | Turna as the entrypoint of an existing image: config from Consul or Vault, the application started with its environment, files written before start, and a proxy in front. |
| [Environment Values](/examples/env) | Loading values with `loads` and templating them into service environments. |
| [Preprocess](/examples/preprocess) | Replacing placeholders in files before serving them. |

## TCP and UDP

| Example | Shows |
| --- | --- |
| [SOCKS5 Proxy](/examples/socks5) | A SOCKS5 proxy on a TCP entrypoint with credentials and hostname mapping. |
| [DNS Server](/examples/dns) | A DNS responder on a UDP entrypoint with static records and upstream fallback. |
