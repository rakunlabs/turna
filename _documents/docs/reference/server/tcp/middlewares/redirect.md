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
          buffer: 65535
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
| `proxy_protocol` | `false` | Send PROXY protocol metadata for TCP upstreams. |
| `buffer` | `65535` | Copy buffer size. |
