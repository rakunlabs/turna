# ip_allow_list

`ip_allow_list` allows only TCP clients whose remote IP is inside one of the configured CIDR ranges.

```yaml
server:
  tcp:
    middlewares:
      local_only:
        ip_allow_list:
          source_range:
            - 127.0.0.1/32
            - 10.0.0.0/8
```

At least one `source_range` entry is required. Put this middleware before `redirect` or `socks5` when it should protect the TCP target.
