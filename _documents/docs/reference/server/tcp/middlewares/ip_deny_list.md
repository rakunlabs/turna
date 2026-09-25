# ip_deny_list

`ip_deny_list` closes connections from the listed IP addresses or CIDR ranges. It is the opposite of [`ip_allow_list`](./ip_allow_list).

```yaml
server:
  tcp:
    middlewares:
      block_bad:
        ip_deny_list:
          source_range:
            - 203.0.113.0/24
            - 198.51.100.7
```

| Field | Default | Description |
| --- | --- | --- |
| `source_range` | | Denied IPs or CIDRs. Required. |

Place it after [`proxy_protocol`](./proxy_protocol) to check the original client address.
