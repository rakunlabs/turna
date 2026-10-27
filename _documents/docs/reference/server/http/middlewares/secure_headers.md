# secure_headers

`secure_headers` sets security response headers with safe defaults. Use [`headers`](./headers) for any other header.

```yaml
server:
  http:
    middlewares:
      security:
        secure_headers:
          hsts:
            max_age: 8760h
            include_subdomains: true
          content_security_policy: "default-src 'self'; script-src 'self' 'nonce-{nonce}'"
          nonce_header: X-CSP-Nonce
          permissions_policy: "camera=(), microphone=(), geolocation=()"
          cross_origin_opener_policy: same-origin
          remove_headers:
            - Server
            - X-Powered-By
```

| Field | Default | Description |
| --- | --- | --- |
| `hsts.max_age` | `8760h` | `Strict-Transport-Security` max age. |
| `hsts.include_subdomains` | `false` | Adds `includeSubDomains`. |
| `hsts.preload` | `false` | Adds `preload`. Needs `include_subdomains` and a max age of at least a year. |
| `hsts.trust_forwarded_proto` | `false` | Also sends HSTS when `X-Forwarded-Proto` is `https`, for TLS terminated in front of Turna. |
| `content_security_policy` | | `Content-Security-Policy` value. `{nonce}` is replaced with a random value per request. |
| `content_security_policy_report_only` | `false` | Sends the policy as `Content-Security-Policy-Report-Only`. |
| `nonce_header` | | Request header that passes the nonce to the next handlers, e.g. a template or upstream. |
| `frame_options` | `DENY` | `X-Frame-Options`. |
| `content_type_options` | `nosniff` | `X-Content-Type-Options`. |
| `referrer_policy` | `strict-origin-when-cross-origin` | `Referrer-Policy`. |
| `permissions_policy` | | `Permissions-Policy`. |
| `cross_origin_opener_policy` | | `Cross-Origin-Opener-Policy`. |
| `cross_origin_resource_policy` | | `Cross-Origin-Resource-Policy`. |
| `cross_origin_embedder_policy` | | `Cross-Origin-Embedder-Policy`. |
| `override` | `false` | Replace headers already set by the upstream. By default they are kept. |
| `remove_headers` | | Response headers to remove. |

Set a field with a default to `-` to leave the header out, e.g. `frame_options: "-"` for pages that are embedded in frames.

HSTS is sent only on HTTPS requests, browsers ignore it over plain HTTP.

With `{nonce}` the upstream must put the same value in the `nonce` attribute of its scripts and styles. Read it from `nonce_header`. Proxied requests carry it to the upstream. The header is set by Turna, so a value sent by the client is replaced.
