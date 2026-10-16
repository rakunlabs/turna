# Docker and Swarm

The Docker provider builds routers, services and middlewares from labels on Docker containers or Swarm services. It watches the Docker API and updates the running server without a restart. Routes from the provider are added next to the static configuration, so static routers, middlewares and the auth stack keep working and can be reused by containers.

Compose projects need nothing beyond labels. Replicas of a compose service are grouped into one load balanced service.

## Quick start

```yaml
# turna.yaml
server:
  entrypoints:
    web:
      address: ":80"

providers:
  docker:
    enabled: true
```

```yaml
# compose.yaml
services:
  turna:
    image: ghcr.io/rakunlabs/turna:latest
    ports: ["80:80"]
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - ./turna.yaml:/turna.yaml:ro

  app:
    image: traefik/whoami
    deploy:
      replicas: 2
    labels:
      turna.enable: "true"
      turna.http.routers.app.host: app.example.com
```

Requests for `app.example.com` are balanced over both replicas on their only exposed port. Containers without `turna.enable=true` are ignored unless `exposed_by_default` is set.

::: warning Docker socket
Access to the Docker socket is equivalent to root on the host. Mount it read-only, or point `endpoint` to a socket proxy such as `tecnativa/docker-socket-proxy` that only allows `GET` requests on `/version`, `/containers`, `/events`, `/networks`, `/services` and `/tasks`. A read-only mount does not limit API calls, only a filtering proxy does.
:::

## Configuration

```yaml
providers:
  docker:
    enabled: true
    endpoint: unix:///var/run/docker.sock
    label_prefix: turna
    exposed_by_default: false
    network: ""
    http:
      entrypoints: []
      middlewares: []
      static_middlewares: []
    tcp:
      entrypoints: []
      middlewares: []
    udp:
      entrypoints: []
      middlewares: []
    swarm:
      enabled: false
      use_vip: false
    refresh_interval: 30s
    debounce: 200ms
```

| Field | Default | Description |
| --- | --- | --- |
| `enabled` | `false` | Starts the provider. |
| `endpoint` | `DOCKER_HOST`, then `unix:///var/run/docker.sock` | Docker API address: `unix://`, `tcp://`, `http://` or `https://`. |
| `api_version` | daemon version | Pins the API version, like `1.47`. |
| `tls.ca`, `tls.cert`, `tls.key` | | Files for a TLS protected `tcp://` or `https://` endpoint. Without `tls`, `DOCKER_TLS_VERIFY` and `DOCKER_CERT_PATH` (`ca.pem`, `cert.pem`, `key.pem`, default `~/.docker`) are used like the docker CLI, and `tcp://` then uses HTTPS. |
| `tls.insecure_skip_verify` | `false` | Skips verification of the daemon certificate. |
| `label_prefix` | `turna` | Prefix of the labels read. Use different prefixes to run several Turna instances on one host. |
| `exposed_by_default` | `false` | Serves containers without `<prefix>.enable`. `<prefix>.enable=false` still opts out. |
| `network` | | Network used to reach containers attached to several networks. Compose and stack project prefixes are added automatically, so `default` matches `myproject_default`. |
| `http.entrypoints` | every HTTP entrypoint | Entrypoints labels may use. Routers without `entrypoints` use the ones of this list serving the same TLS mode. |
| `http.middlewares` | safe set, see below | Middleware types labels may define. `["*"]` allows every type. |
| `http.static_middlewares` | none | Middlewares of the static configuration labels may reference by name, like `[auth, login]`. `["*"]` allows every one. |
| `tcp.entrypoints` | none | Entrypoints TCP routers from labels may use. TCP routers are disabled until this is set, because a TCP router takes over the whole entrypoint. |
| `tcp.middlewares` | safe set | Middleware types TCP labels may define. |
| `tcp.static_middlewares` | none | Static TCP middlewares labels may reference. |
| `udp.entrypoints` | none | Entrypoints UDP routers from labels may use. |
| `udp.middlewares` | safe set | Middleware types UDP labels may define. |
| `udp.static_middlewares` | none | Static UDP middlewares labels may reference. |
| `swarm.enabled` | `false` | Reads Swarm services and tasks instead of containers. Turna must reach a manager node. |
| `swarm.use_vip` | `false` | Proxies to the service virtual IP instead of the task IPs. |
| `refresh_interval` | `30s`, `10s` with Swarm | Full re-read besides events. Swarm tasks on other nodes have no events here, so this bounds how fast they get or lose traffic. |
| `debounce` | `200ms` | Collects events before applying. |

## Labels

Labels follow the shape of the server configuration below `<prefix>.`. Values are strings; numbers, booleans and durations are converted, and lists are comma separated or indexed (`prefixes.0`, `prefixes.1`). Fields holding patterns (names with `match`, `regex` or `pattern`) are not split on commas, use indexed labels for several patterns. A section like `health_check` can be enabled with its defaults by `health_check=true`.

```yaml
labels:
  turna.enable: "true"
  turna.network: backend

  # http router
  turna.http.routers.api.host: api.example.com,www.example.com
  turna.http.routers.api.path: /api/*
  turna.http.routers.api.entrypoints: websecure
  turna.http.routers.api.tls: "true"
  turna.http.routers.api.middlewares: strip,auth
  turna.http.routers.api.service: api

  # service of the router
  turna.http.services.api.port: "8080"

  # middleware defined by the container
  turna.http.middlewares.strip.strip_prefix.prefix: /api
```

`auth` is not defined by the container, so it refers to a middleware of the static configuration. A container can reuse the login, session and auth middlewares of Turna without seeing their configuration, once they are listed in `http.static_middlewares`:

```yaml
providers:
  docker:
    enabled: true
    http:
      static_middlewares: [auth]
```

### General

| Label | Description |
| --- | --- |
| `<prefix>.enable` | `true` serves the container, `false` ignores it. |
| `<prefix>.network` | Network used to reach this container, overrides `network`. |

### HTTP routers

| Label | Default | Description |
| --- | --- | --- |
| `http.routers.<name>.host` | every host | Host names, comma separated. Wildcards like `*.example.com` are supported. |
| `http.routers.<name>.path` | `/*` | Paths, comma separated. |
| `http.routers.<name>.entrypoints` | see `http.entrypoints` | Entrypoints of the router. Without them, routers use every HTTP entrypoint serving the same TLS mode. |
| `http.routers.<name>.tls` | `false` | Serves the router over TLS with the certificates of the static configuration. |
| `http.routers.<name>.middlewares` | | Middlewares run before the service, in order. Names of the container's middlewares, or of the static configuration listed in `http.static_middlewares`. |
| `http.routers.<name>.service` | the only service | Service of the router. Required when the container defines several services. |

### HTTP services

A router without any service label uses a default service on the container's only exposed port.

| Label | Default | Description |
| --- | --- | --- |
| `http.services.<name>.port` | the only exposed port | Port of the container. |
| `http.services.<name>.scheme` | `http` | `http` or `https`. |
| `http.services.<name>.h2c` | `false` | HTTP/2 without TLS, required for gRPC backends. |
| `http.services.<name>.pass_host_header` | `true` | Sends the original `Host` header. |
| `http.services.<name>.server_name` | | SNI name verified on `https` upstreams. |
| `http.services.<name>.insecure_skip_verify` | `false` | Skips verification of `https` upstreams. |
| `http.services.<name>.retry` | `2` | Retries on another replica when one is unreachable. |
| `http.services.<name>.strategy` | `round_robin` | `round_robin`, `random` or `least_conn`. |
| `http.services.<name>.sticky.cookie.*` | | Session affinity, see the [service middleware](./server/http/middlewares/service). |
| `http.services.<name>.health_check.*` | | Active health check, `health_check=true` uses the defaults. |
| `http.services.<name>.passive_health_check.*` | | Ejects failing replicas, for example `passive_health_check.max_fails=3`. |

### Middlewares

`http.middlewares.<name>.<type>.<field>` defines a middleware with the same fields as the static configuration. Type names match loosely, so `strip_prefix`, `strip-prefix` and `stripPrefix` are the same.

```yaml
labels:
  turna.http.middlewares.hdr.headers.custom_response_headers.X-Frame-Options: DENY
  turna.http.middlewares.cors.cors.allow_origins: https://app.example.com
  turna.http.middlewares.limit.rate_limit.requests: "100"
  turna.http.middlewares.limit.rate_limit.duration: 1m
```

Labels can be set by anyone who can start a container, so only middlewares that transform or filter traffic are allowed by default:

| Kind | Allowed by default |
| --- | --- |
| HTTP | `add_prefix`, `strip_prefix`, `regex_path`, `headers`, `cors`, `basic_auth`, `block`, `gzip`, `compress`, `decompress`, `rate_limit`, `redirection`, `request_id`, `access_log`, `log`, `url`, `csrf` |
| TCP | `ip_allow_list`, `ip_deny_list`, `conn_limit`, `rate_limit`, `idle_timeout`, `bandwidth_limit`, `proxy_protocol`, `log`, `block` |
| UDP | `ip_allow_list`, `ip_deny_list`, `rate_limit`, `log`, `block` |

Types that read files or loaded data of Turna (`folder`, `template`, `inject`, `view`), hold credentials (`auth`, `login`, `session`, `gcp_token`) or reach other hosts (`service`, `forward`, `request`) need an explicit allow. Prefer defining them in the static configuration and listing them in `static_middlewares`.

### TCP and UDP

TCP and UDP routers need entrypoints listed in `tcp.entrypoints` or `udp.entrypoints`. A TCP router takes over the whole entrypoint, so only one container can use it.

```yaml
providers:
  docker:
    enabled: true
    tcp:
      entrypoints: [postgres]
```

```yaml
labels:
  turna.enable: "true"
  turna.tcp.routers.db.entrypoints: postgres
  turna.tcp.routers.db.middlewares: office
  turna.tcp.middlewares.office.ip_allow_list.source_range: 10.0.0.0/8
  turna.tcp.services.db.port: "5432"
```

| Label | Description |
| --- | --- |
| `tcp.routers.<name>.entrypoints` | Required. Entrypoints of the router. |
| `tcp.routers.<name>.middlewares` | Middlewares run before the service. |
| `tcp.routers.<name>.service` | Service of the router. |
| `tcp.services.<name>.port` | Port of the container. |
| `tcp.services.<name>.*` | `strategy`, `retry`, `dial_timeout`, `disable_nagle`, `proxy_protocol`, `proxy_protocol_version`, `health_check.*`, `passive_health_check.*` of the [load_balancer](./server/tcp/middlewares/load_balancer). |
| `udp.routers.<name>.*` | Same as TCP. |
| `udp.services.<name>.*` | `port`, `strategy`, `session_timeout`, `max_sessions`, `health_check.*`, `passive_health_check.*` of the UDP load balancer. |

## Swarm

With `swarm.enabled`, labels are read from the service, which in a stack file means `deploy.labels`, not the container `labels`. Turna proxies to the IP of every running task, so its load balancing, retries and health checks see each replica. With `use_vip` the service virtual IP is used instead and Swarm balances the connections.

```yaml
# stack.yaml
services:
  turna:
    image: ghcr.io/rakunlabs/turna:latest
    ports:
      - target: 80
        published: 80
        mode: host
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    configs:
      - source: turna
        target: /turna.yaml
    networks: [app]
    deploy:
      placement:
        constraints: [node.role == manager]

  api:
    image: traefik/whoami
    networks: [app]
    deploy:
      replicas: 3
      labels:
        turna.enable: "true"
        turna.http.routers.api.host: api.example.com
        turna.http.services.api.port: "80"

networks:
  app:
    driver: overlay

configs:
  turna:
    file: ./turna.yaml
```

```yaml
# turna.yaml
server:
  entrypoints:
    web:
      address: ":80"
providers:
  docker:
    enabled: true
    swarm:
      enabled: true
    network: app
```

Turna must be attached to the networks of the services it routes to. Swarm only reports published ports, not the ports exposed by the image, so set `port` on services unless the service publishes exactly one port.

## Runtime behavior

- Containers are discovered when they start and removed when they stop. Containers with a failing or starting health check get no traffic; when no replica of a group is healthy, its routers stay and answer `503`. In Swarm mode, tasks get traffic once Swarm reports them running, which waits for a passing health check.
- Containers with `network_mode: host` are reached on `127.0.0.1`, which only works when Turna runs on the host network too. They list no ports, so set `port`.
- Replicas of a compose service (same `com.docker.compose.project` and `com.docker.compose.service`) form one group. During a rolling update the labels of the newest replica are used.
- When two containers claim the same host and path, the older one keeps it and the newer one is logged and ignored.
- Hosts served by the static configuration are reserved: containers cannot add any path to them, which would bypass the middlewares of the static routers. Routers without a host are rejected when the static configuration has a router without host on the same entrypoint.
- A container with invalid labels is logged and ignored; the other containers are not affected. A router with a problem is dropped without dropping the rest of the container.
- When the Docker API is unreachable, the last configuration keeps serving and the provider reconnects with backoff.

Generated names start with `docker@`, for example `docker@shop/web@http@api`. Do not start names of the static configuration with `docker@`.
