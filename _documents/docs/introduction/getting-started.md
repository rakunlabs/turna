# Getting Started

Turna runs from a single configuration file. A configuration can load external data, preprocess files, start HTTP/TCP entrypoints, and run local commands.

## Install

Download the latest release binary for your platform.

```sh
curl -fSL https://github.com/rakunlabs/turna/releases/latest/download/turna_Linux_x86_64.tar.gz | tar -xz --overwrite -C ~/bin/ turna
```

Homebrew is also supported.

```sh
brew tap brew-tools/tap
brew install turna
```

## First Config

Create `turna.yaml` in the directory where you run `turna`.

```yaml
log_level: info

server:
  entrypoints:
    web:
      address: ":8080"
  http:
    middlewares:
      app:
        service:
          loadbalancer:
            servers:
              - url: "http://localhost:3000"
    routers:
      app:
        path: /*
        middlewares:
          - app
```

Start Turna.

```sh
turna --config-file
```

Requests to `http://localhost:8080/*` are proxied to `http://localhost:3000/*`.

## Static Files

Turna can also serve a folder directly.

```yaml
server:
  entrypoints:
    web:
      address: ":8080"
  http:
    middlewares:
      files:
        folder:
          path: ./dist
          index: true
          spa: true
    routers:
      files:
        path: /*
        middlewares:
          - files
```

## Run Commands

Use `services` when Turna should also start local processes.

```yaml
services:
  - name: migrate
    command: ./migrate.sh
    order: 0
  - name: app
    command: ./app
    depends:
      - migrate
```

## Next Steps

- Read the [configuration reference](/reference/config) for load order and top-level sections.
- Read the [server reference](/reference/server/server) for entrypoints, routers, TLS, and middleware chains.
- Read the [HTTP middleware index](/reference/server/http/middlewares/) for all supported middleware keys.
- Read [services](/reference/services) for command execution and dependency behavior.
