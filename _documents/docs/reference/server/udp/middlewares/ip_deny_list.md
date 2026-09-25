# ip_deny_list

`ip_deny_list` drops datagrams from the listed IP addresses or CIDR ranges. It is the opposite of [`ip_allow_list`](./ip_allow_list).

```yaml
server:
  udp:
    middlewares:
      block_bad:
        ip_deny_list:
          source_range:
            - 203.0.113.0/24
```

| Field | Default | Description |
| --- | --- | --- |
| `source_range` | | Denied IPs or CIDRs. Required. |
