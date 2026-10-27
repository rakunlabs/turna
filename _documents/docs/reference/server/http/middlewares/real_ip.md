# real_ip

`real_ip` finds the client IP behind trusted proxies and makes it the request address. Middlewares after it (`ip_filter`, `rate_limit`, `concurrency_limit`, `access_log`, `service` and others) see the client instead of the load balancer, without their own `trusted_proxies`.

```yaml
server:
  http:
    middlewares:
      client_ip:
        real_ip:
          trusted_proxies:
            - 10.0.0.0/8
            - 172.16.0.0/12
    routers:
      app:
        path: /*
        middlewares:
          - client_ip
          - internal_only
          - backend
```

| Field | Description |
| --- | --- |
| `trusted_proxies` | Required. IPs or CIDRs of proxies in front of Turna. |
| `header` | Read the client IP only from this header, e.g. `CF-Connecting-IP`, instead of `X-Forwarded-For`. |

## Behavior

When the connection comes from a trusted proxy:

- `X-Forwarded-For` is read from right to left and the first address that is not a trusted proxy is the client. Without it `True-Client-IP` and then `X-Real-IP` are used. With `header`, only that header is read.
- The client becomes the request address, `X-Real-IP` is set to it and the hops up to the client are removed from `X-Forwarded-For`, so `service` sends a correct chain upstream.
- A malformed header gets `400 Bad Request`.
- Without any forwarding header the proxy itself is the client.

When the connection comes from any other address, `X-Forwarded-For`, `X-Real-IP`, `True-Client-IP` and `header` are removed and `X-Real-IP` is set to the connection address, so clients cannot forge them.

Put `real_ip` first in the chain.
