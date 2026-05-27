# forward

`forward` turns an HTTP entrypoint into a forward proxy. It supports regular HTTP proxy requests and `CONNECT` tunnels.

```yaml
server:
  entrypoints:
    proxy:
      address: ":9292"
  http:
    middlewares:
      forward_proxy:
        forward:
          insecure_skip_verify: false
    routers:
      proxy:
        path: /*
        middlewares:
          - forward_proxy
```

| Field | Default | Description |
| --- | --- | --- |
| `insecure_skip_verify` | `false` | Skip upstream TLS verification. |

Configure clients as if using an `http_proxy` or `https_proxy` endpoint.
