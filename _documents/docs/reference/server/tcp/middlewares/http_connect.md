# http_connect

`http_connect` is an HTTP `CONNECT` tunnel proxy, the kind used by `HTTPS_PROXY` settings.

```yaml
server:
  tcp:
    middlewares:
      proxy:
        http_connect:
          users:
            alice: secret
          allowed_hosts:
            - "*.example.com"
            - api.github.com
          allowed_ports: [443]
          dial_timeout: 10s
    routers:
      proxy:
        entrypoints: [proxy]
        middlewares: [proxy]
```

| Field | Default | Description |
| --- | --- | --- |
| `users` | | Basic auth users for `Proxy-Authorization` (`user: password`). When empty no authentication is required. |
| `allowed_hosts` | all | Allowed target hosts. `*.example.com` matches subdomains, `*` matches all. |
| `allowed_ports` | all | Allowed target ports. |
| `dial_timeout` | `10s` | Timeout to connect to the target. |
| `timeout` | `10s` | Timeout to read the `CONNECT` request. |

Other methods get `405`, failed authentication `407`, disallowed targets `403` and unreachable targets `502`. Use it behind [`tls_terminate`](./tls_terminate) to protect credentials, or with [`ip_allow_list`](./ip_allow_list).
