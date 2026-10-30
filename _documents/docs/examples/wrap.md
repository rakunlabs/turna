# Wrap an Application

Turna can be the entrypoint of any container. It reads its configuration from Consul or Vault, writes the files the application needs, starts the application with the right environment, and puts a proxy in front of it. The image stays the upstream one with the Turna binary added, and everything environment specific lives in the configuration store.

## Add Turna to an image

The Turna release binary is static, so it runs in any Linux image. Copy it from the Turna image and make it the entrypoint:

```dockerfile
FROM n8nio/n8n:1.110.1

USER root
COPY --from=ghcr.io/rakunlabs/turna:latest /turna /usr/local/bin/turna
ENTRYPOINT ["turna"]
```

The original entrypoint of the image is no longer run, so the configuration starts the application itself as a [service](/reference/services). When the container stops, Turna sends `SIGTERM` to every service and kills it after `stop_timeout`.

## Config from Consul or Vault

Environment variables choose where Turna reads its configuration, see [application config sources](/reference/config#application-config-sources). The document name is `turna` by default; `APP_NAME` and `CONFIG_NAME_PREFIX` change it, so many applications share one store.

```yaml
# compose.yaml
services:
  workflows:
    image: registry.example.com/platform/workflows:1.110.1
    environment:
      # Vault KVv2 secret <mount>/platform/workflows
      VAULT_ADDR: https://vault.example.com
      VAULT_SECRET_BASE_PATH: secret
      VAULT_ROLE_ID: 00000000-0000-0000-0000-000000000000
      VAULT_ROLE_SECRET: change-me
      CONFIG_NAME_PREFIX: platform
      APP_NAME: workflows

  grafana:
    image: registry.example.com/platform/grafana:12.1.0
    environment:
      # Consul key platform/grafana/turna
      CONSUL_HTTP_ADDR: consul:8500
      CONSUL_CONFIG_PATH_PREFIX: platform/grafana
```

Keep a configuration in Vault when it holds passwords or keys, and in Consul otherwise. A Vault secret holds the configuration as its key/value data, the same tree as the YAML file. Set `LOG_LEVEL=debug` to see which sources were read.

## Run the application with its environment

The workflow tool reads all of its settings from environment variables. Instead of listing them in the compose file of every environment, the Vault document `platform/workflows` holds them next to the database password:

```yaml
services:
  - name: n8n
    command: n8n
    inherit_env: true
    env:
      DB_TYPE: postgresdb
      DB_POSTGRESDB_HOST: postgres
      DB_POSTGRESDB_PORT: "5432"
      DB_POSTGRESDB_DATABASE: workflows
      DB_POSTGRESDB_SCHEMA: public
      DB_POSTGRESDB_USER: workflows_app
      DB_POSTGRESDB_PASSWORD: change-me
      N8N_HOST: workflows.example.com
      N8N_PORT: "5678"
      N8N_PROTOCOL: http
      WEBHOOK_URL: https://workflows.example.com/
      N8N_DIAGNOSTICS_ENABLED: "false"
      N8N_VERSION_NOTIFICATIONS_ENABLED: "false"
      GENERIC_TIMEZONE: Europe/Amsterdam
      TZ: Europe/Amsterdam
```

`inherit_env` keeps the variables of the image, such as `PATH` and `NODE_VERSION`, and `env` adds the rest. The variables only exist in the process Turna starts, so they do not show up in `docker inspect`. Rotating the password is a change in Vault and a restart.

## Write configuration files

Some applications want files instead of variables. [`loads`](/reference/loads) fetches data before the services start and can write it to a file. Here Grafana gets its datasources from a separate Consul key and runs with its own user:

```yaml
# Consul key platform/grafana/turna
loads:
  - export: /etc/grafana/provisioning/datasources/datasources.yaml
    statics:
      - consul:
          path: grafana/datasources
          path_prefix: platform

services:
  - name: grafana
    path: /
    command: /run.sh
    user: "472"
    inherit_env: true
    env:
      GF_SERVER_ROOT_URL: https://apps.example.com/grafana/
      GF_SERVER_SERVE_FROM_SUB_PATH: "true"
      GF_AUTH_ANONYMOUS_ENABLED: "true"
      GF_AUTH_ANONYMOUS_ORG_ROLE: Viewer
```

```yaml
# Consul key platform/grafana/datasources
apiVersion: 1
datasources:
  - name: Loki
    type: loki
    url: http://loki:3100
  - name: Tempo
    type: tempo
    url: http://tempo:3200
```

Changing a datasource is a change of one Consul key, without a new image. Use `dynamics` instead of `statics` to rewrite the file while the container runs.

## Put a proxy in front

With a `server` section, Turna also serves HTTP next to the application. The application listens on localhost, and every request passes through the middlewares first. This fixes behavior of an application without changing its image:

```yaml
server:
  entrypoints:
    web:
      address: ":8080"
  http:
    middlewares:
      embed:
        headers:
          custom_response_headers:
            X-Frame-Options: ""
            Content-Security-Policy: frame-ancestors 'self' https://portal.example.com
      links:
        inject:
          content_types:
            - text/html
          path_map:
            "**":
              - old: href="/my/page"
                new: href="/projects/home"
      sso:
        service:
          loadbalancer:
            servers:
              - url: http://auth:8080
      app:
        service:
          loadbalancer:
            servers:
              - url: http://localhost:3000
    routers:
      sso:
        path: /auth/oauth2/*
        middlewares:
          - sso
      app:
        path: /*
        middlewares:
          - embed
          - links
          - app

services:
  - name: tracker
    command: /docker-entrypoint.sh rails server -b 127.0.0.1 -p 3000
    inherit_env: true
    env:
      DATABASE_URL: postgres://tracker:change-me@postgres:5432/tracker
  - name: cron
    command: cron -f
    inherit_env: true
```

- `embed` removes `X-Frame-Options` and allows the portal to show the application in an iframe.
- `links` rewrites a link in the HTML pages of the application.
- `sso` sends the OAuth2 paths to the identity service of the [Auth example](/examples/auth), so the application can use it as its OpenID provider on its own host.
- Only port `8080` is published; the application on `localhost:3000` is not reachable from outside.
- Two services run side by side, the web server and its cron daemon. When one fails, Turna stops the other and exits, so the orchestrator restarts the container.

[`session`](/reference/server/http/middlewares/session) and [`iam_check`](/reference/server/http/middlewares/iam_check) can be added to the `app` router the same way to put the login of the platform in front of an application that has none, see [Single Page App](/examples/spa).
