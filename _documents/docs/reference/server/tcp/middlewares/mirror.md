# mirror

`mirror` copies the data clients send to shadow servers and continues the chain. Replies of the shadow servers are discarded, so they never reach the client. Use it to try a new version of a service with real traffic.

```yaml
server:
  tcp:
    middlewares:
      shadow:
        mirror:
          servers:
            - address: 10.0.0.50:6379
              percent: 10
      backend:
        redirect:
          address: 10.0.0.1:6379
    routers:
      redis:
        entrypoints: [redis]
        middlewares: [shadow, backend]
```

| Field | Default | Description |
| --- | --- | --- |
| `servers[].address` | | Shadow server `host:port`. |
| `servers[].percent` | `100` | Percent of connections to mirror. |
| `dial_timeout` | `2s` | Dial timeout of shadow servers. |
| `buffer` | `1048576` | Bytes queued per shadow connection. |

Each mirrored client connection opens its own connection to the shadow server. Shadow servers never slow the real connection down: if one cannot be reached or reads slower than the client sends and its `buffer` fills up, its copy stops for that connection. Mirroring is best effort; errors are only logged at debug level.

Only data sent by the client is mirrored. Place `mirror` before `tls_terminate` to mirror encrypted data, after it to mirror plain data, and before the terminal middleware.
