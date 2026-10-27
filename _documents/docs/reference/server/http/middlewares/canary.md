# canary

`canary` splits traffic between middleware chains by weight. Clients can stay on the same variant, and a header or cookie can pick a variant for testing.

```yaml
server:
  http:
    middlewares:
      app_v1:
        service:
          loadbalancer:
            servers:
              - url: http://app-v1:8080
      app_v2:
        service:
          loadbalancer:
            servers:
              - url: http://app-v2:8080
      app:
        canary:
          variants:
            - name: stable
              weight: 95
              middlewares:
                - app_v1
            - name: beta
              weight: 5
              middlewares:
                - app_v2
          sticky: {}
          override_header: X-Canary
          variant_header: X-Variant
    routers:
      app:
        path: /*
        middlewares:
          - app
```

| Field | Default | Description |
| --- | --- | --- |
| `variants[].name` | | Required. Unique name of the variant. |
| `variants[].weight` | | Share of traffic, relative to the other variants. `0` sends no new traffic. |
| `variants[].middlewares` | | Required. Chain that handles the variant, usually ending with `service`. |
| `sticky` | | Keep a client on the same variant, see below. |
| `override_header` | | Request header naming the variant to use, e.g. `X-Canary: beta`. |
| `override_cookie` | | Cookie naming the variant to use. |
| `variant_header` | | Request header set to the selected variant, for logs or the upstream. |

At least two variants are required and one must have weight. A variant chain that does not answer continues with the middlewares after `canary` in the router.

## Sticky

Without `sticky` each request is assigned at random.

With `sticky: {}` the variant is stored in a cookie on the first request. When the weight of a stored variant drops to `0`, the client is assigned again, so a variant can be drained.

| Field | Default | Description |
| --- | --- | --- |
| `sticky.cookie` | `turna_canary` | Cookie name. |
| `sticky.max_age` | `24h` | Cookie lifetime. |
| `sticky.path` | `/` | Cookie path. |
| `sticky.domain` | | Cookie domain. |
| `sticky.secure` | `false` | Send the cookie over HTTPS only. |
| `sticky.key` | | Pick the variant by hashing request parts instead of a cookie. |
| `sticky.trusted_proxies` | | CIDRs whose forwarding headers are used for the `ip` key. |

`sticky.key` accepts the parts of [`rate_limit`](./rate_limit#keys), e.g. `header:X-User-Id` or `ip`. The same key always gets the same variant while the weights stay the same, also across Turna instances and without cookies.

Overrides win over sticky assignment.
