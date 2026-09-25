# redirect

`redirect` connects each accepted TCP connection to another address and copies bytes in both directions.

```yaml
server:
  entrypoints:
    docker:
      address: ":2375"
  tcp:
    middlewares:
      docker_socket:
        redirect:
          address: /var/run/docker.sock
          network: unix
          disable_nagle: false
          dial_timeout: 10s
          proxy_protocol: false
    routers:
      docker:
        entrypoints:
          - docker
        middlewares:
          - docker_socket
```

| Field | Default | Description |
| --- | --- | --- |
| `address` | | Upstream address. |
| `network` | `tcp` | Upstream network: `tcp`, `tcp4`, `tcp6`, `unix`, `unixpacket`, `udp`, `udp4`, or `udp6`. |
| `disable_nagle` | `false` | Disable Nagle's algorithm for TCP connections. |
| `dial_timeout` | none | Timeout for dialing the upstream. |
| `proxy_protocol` | `false` | Send a PROXY protocol header to TCP upstreams. |
| `proxy_protocol_version` | `1` | PROXY protocol version: `1` (text) or `2` (binary). |
| `buffer` | | Deprecated and ignored. Data is copied with the kernel `splice`/`sendfile` fast path when possible. |

`redirect` is terminal: middlewares after it in the router are not run. When an earlier middleware wraps the connection (for example `tls_terminate`), the decrypted stream is forwarded. The PROXY header carries the client address seen by the chain, so it is the original client when [`proxy_protocol`](./proxy_protocol) is used before it.
