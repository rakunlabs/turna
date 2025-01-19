# Redirect

Redirect middleware is used to redirect the request to a different address and network.

```yaml
server:
  tcp:
    middlewares:
      redirect:
        redirect:
          address: "example.com:22" # Address to redirect to should be in the format with network
          network: "tcp" # Could be "tcp", "tcp4", "tcp6", "unix", "unixpacket", "udp", "udp4", "udp6"
          disable_nagle: false # Disable Nagle's algorithm
          dial_timeout: "10s" # Timeout for the connection, default is none
          proxy_protocol: false # Enable PROXY protocol
          buffer: 65535 # Buffer size for the connection default is 65535 (64KB)
```

Example configuration:

```yaml
server:
  entrypoints:
    ssh:
      address: ":8822"
  tcp:
    middlewares:
      redirect:
        redirect:
          address: "example.com:22"
    routers:
      mytcprouter:
        entrypoints:
          - ssh
        middlewares:
          - redirect
```
