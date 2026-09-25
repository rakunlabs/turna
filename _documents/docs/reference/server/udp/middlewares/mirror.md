# mirror

`mirror` sends a copy of every datagram to shadow servers and continues the chain. Replies of the shadow servers are discarded, so they never reach the client.

```yaml
server:
  udp:
    middlewares:
      shadow:
        mirror:
          servers:
            - address: 10.0.0.50:53
              percent: 10
      backend:
        load_balancer:
          servers:
            - address: 10.0.0.1:53
    routers:
      dns:
        entrypoints: [dns]
        middlewares: [shadow, backend]
```

| Field | Default | Description |
| --- | --- | --- |
| `servers[].address` | | Shadow server `host:port`. |
| `servers[].percent` | `100` | Percent of datagrams to mirror. |
| `network` | `udp` | `udp`, `udp4` or `udp6`. |

Place `mirror` before the terminal middleware. Mirroring is best effort; send errors are only logged at debug level.
