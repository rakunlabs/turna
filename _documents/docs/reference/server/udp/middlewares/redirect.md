# Redirect

`redirect` proxies datagrams on a UDP entrypoint to an upstream UDP server. For each incoming datagram it dials the target, forwards the payload, waits for one reply within `timeout`, and writes the reply back to the peer.

This fits single request/response protocols such as DNS. Protocols that answer with multiple datagrams per request are not supported.

```yaml
server:
  udp:
    middlewares:
      upstream_dns:
        redirect:
          address: 1.1.1.1:53
          network: udp
          timeout: 5s
          buffer: 65535
    routers:
      dns:
        entrypoints:
          - dns
        middlewares:
          - upstream_dns
```

| Field | Default | Description |
| --- | --- | --- |
| `address` | | Upstream `host:port` to forward datagrams to. |
| `network` | `udp` | Network used to dial the upstream (`udp`, `udp4`, `udp6`). |
| `timeout` | `5s` | Timeout for dialing the upstream and waiting for the reply. |
| `buffer` | `65535` | Read buffer size for the upstream reply. |

`redirect` is a terminal middleware: it writes the response and should be the last entry in the router chain.
