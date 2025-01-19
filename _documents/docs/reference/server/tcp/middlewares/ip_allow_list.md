# IP Allow List

The IP Allow List middleware allows you to restrict access to your server to a list of IP addresses or CIDR ranges.

```yaml
server:
  tcp:
    middlewares:
      ip:
        ip_allow_list:
          source_range: # source_range []string
            - 127.0.0.1/32
```

Example configuration:

```yaml
server:
  entrypoints:
    docker:
      address: ":2375"
  tcp:
    middlewares:
      ip:
        ip_allow_list:
          source_range:
            - 127.0.0.1/32
      redirect:
        redirect:
          address: "/var/run/docker.sock"
          network: "unix"
    routers:
      mytcprouter:
        entrypoints:
          - docker
        middlewares:
          - ip
          - redirect
```
