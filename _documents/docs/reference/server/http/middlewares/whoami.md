# whoami

`whoami` answers with the hostname, IPs and the incoming request, like [`traefik/whoami`](https://github.com/traefik/whoami). It does not call the next middleware. It is useful for testing routing, load balancing, headers set by other middlewares and TLS.

```yaml
server:
  entrypoints:
    web:
      address: ":80"
  http:
    middlewares:
      whoami:
        whoami:
          name: turna
    routers:
      whoami:
        path: /*
        middlewares:
          - whoami
```

```sh
$ curl http://localhost/foo
Name: turna
Hostname: 5f3b1c2a9d4e
IP: 127.0.0.1
IP: 172.18.0.3
RemoteAddr: 172.18.0.1:51234
GET /foo HTTP/1.1
Host: localhost
Accept: */*
User-Agent: curl/8.10.1
```

## Fields

| Field | Default | Description |
| --- | --- | --- |
| `name` | | Printed as `Name` in responses. |
| `env` | `false` | Allow printing the process environment with `?env=true`. |
| `max_body_size` | `1MB` | Maximum request body echoed back, in bytes. |
| `max_data_size` | `10MB` | Maximum size served by `/data`, in bytes. |
| `max_wait` | `30s` | Upper bound for `?wait`. |

::: warning Environment
The environment usually holds secrets. Enable `env` only on test instances.
:::

## Endpoints

Endpoints are selected by the last path segment, so they work behind any router prefix: `/api`, `/app/api` and `/v1/whoami/api` all answer as `/api`, without `strip_prefix`.

| Path | Description |
| --- | --- |
| `.../api` | Same information as JSON. |
| `.../health` | Answers with the current health status, `200` at start. `POST` or `PUT` a status code between `200` and `599`, e.g. `503`, to change it. |
| `.../data` | Answers with `size` × `unit` bytes. `unit` is `B` (default), `KB`, `MB` or `GB`; `attachment=true` sends it as a download. |
| `.../bench` | Answers `1`, for load testing. |
| any other | Text output above; the request body is printed after the headers. |

Every endpoint accepts `?wait=<duration>`, e.g. `?wait=2s`, to delay the response.

```sh
curl -X POST -d 503 http://localhost/health
curl "http://localhost/data?size=5&unit=MB" -o /dev/null
curl "http://localhost/api?wait=500ms"
```

## As a container

The `turna whoami` command runs the same handler on its own, without a config file, so the Turna image can replace `traefik/whoami` in test setups:

```yaml
services:
  whoami:
    image: ghcr.io/rakunlabs/turna:latest
    command: ["whoami", "--name", "app"]
```

See [CLI](../../../../introduction/cli#whoami-command) for its flags.
