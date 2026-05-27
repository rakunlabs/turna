# Features

Turna combines four operational building blocks in one binary.

## Configuration Loader

Turna loads its own configuration from files, environment variables, Consul, and Vault. The `loads` section can also fetch extra data and expose it to templates, generated files, server configuration, or service environment variables.

## Preprocess

Preprocess modules run after `loads` and before the server or services start. The current module is `replace`, which can rewrite files using static strings, regular expressions, templates, or values loaded into memory.

## Server

The server layer provides named entrypoints and routers. HTTP routers run ordered middleware chains; TCP routers run connection middleware chains.

Common HTTP use cases include reverse proxying, static file serving, CORS, headers, compression, request rewrites, OAuth/session flows, IAM checks, and access logging.

Common TCP use cases include TCP redirects, Unix socket exposure, SOCKS5 proxying, and IP allow-listing.

## Service Runner

The service runner starts local commands with templated environment variables. Services can be ordered, run in parallel by order group, depend on other services, ignore allowed failures, inherit the process environment, and filter command output.
