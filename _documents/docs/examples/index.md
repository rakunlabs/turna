# Examples

Complete, runnable configurations for common Turna setups. Each example is a single `turna.yaml`; start it with:

```sh
turna --config-file turna.yaml
```

## Serving and routing

| Example | Shows |
| --- | --- |
| [Reverse Proxy](/examples/reverse_proxy) | API gateway with `service`, `strip_prefix`, `cors`, `gzip`, `rate_limit`, and `access_log`. |
| [File Server](/examples/folder) | Static files and SPA serving with the `folder` middleware. |
| [TLS](/examples/tls) | HTTPS entrypoints with self-signed certificates or automatic ACME (Let's Encrypt). |
| [View](/examples/view) | Swagger documentation UI with the `view` middleware. |

## Authentication

| Example | Shows |
| --- | --- |
| [Auth](/examples/auth) | The PostgreSQL-backed `auth` middleware with `session` and `login` for a complete identity provider. |
| [Basic Auth](/examples/basic_auth) | Protecting paths with htpasswd-style `basic_auth`. |
| [Login](/examples/login) | Browser sessions against an external OAuth2 provider with `session`, `login`, and `role_check`. |
| [OAuth2 With IAM](/examples/oauth2) | The legacy `iam` + `oauth2` issuer stack. |

## Configuration and processes

| Example | Shows |
| --- | --- |
| [Environment Values](/examples/env) | Loading values with `loads` and templating them into service environments. |
| [Preprocess](/examples/preprocess) | Replacing placeholders in files before serving them. |

## TCP and UDP

| Example | Shows |
| --- | --- |
| [SOCKS5 Proxy](/examples/socks5) | A SOCKS5 proxy on a TCP entrypoint with credentials and hostname mapping. |
| [DNS Server](/examples/dns) | A DNS responder on a UDP entrypoint with static records and upstream fallback. |
