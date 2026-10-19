# ip_filter

`ip_filter` allows or denies requests by client IP.

```yaml
server:
  http:
    middlewares:
      internal_only:
        ip_filter:
          allow:
            - 10.0.0.0/8
            - 192.168.1.10
          deny:
            - 10.0.5.0/24
          trusted_proxies:
            - 172.16.0.0/12
```

| Field | Default | Description |
| --- | --- | --- |
| `allow` | | IPs or CIDRs. When set, only matching clients pass. |
| `deny` | | IPs or CIDRs that are rejected. Checked before `allow`. |
| `trusted_proxies` | | CIDRs of proxies in front of Turna, see below. |
| `status_code` | `403` | Status of rejected requests, e.g. `404` to hide the route. |

At least one `allow` or `deny` entry is required. IPv4 and IPv6 are supported.

## Client IP

Without `trusted_proxies` the connection address is used and forwarding headers are ignored, so clients cannot forge them.

Behind a load balancer, list its addresses in `trusted_proxies`. When the connection comes from a trusted proxy, `X-Forwarded-For` is read from right to left and the first address that is not a trusted proxy is the client. `True-Client-IP` and `X-Real-IP` are used only when `X-Forwarded-For` is missing. A malformed header from a trusted proxy rejects the request.
