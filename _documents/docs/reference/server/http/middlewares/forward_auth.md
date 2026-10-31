# forward_auth

`forward_auth` asks an external service whether a request may pass, like Traefik `ForwardAuth` and nginx `auth_request`. It works with Authelia, Authentik, oauth2-proxy or any service of your own.

For each request the auth service gets a request with the client headers and `X-Forwarded-*` headers describing the original request:

- `2xx`: the request continues; headers of `auth_response_headers` are copied to it.
- anything else: its response, e.g. `401` or a `302` to a login page, is sent to the client.

```yaml
server:
  http:
    middlewares:
      authelia:
        forward_auth:
          address: http://authelia:9091/api/authz/forward-auth
          auth_response_headers:
            - Remote-User
            - Remote-Groups
            - Remote-Email
    routers:
      app:
        host: app.example.com
        middlewares:
          - authelia
          - backend
```

```yaml
# oauth2-proxy
oauth2_proxy:
  forward_auth:
    address: http://oauth2-proxy:4180/oauth2/auth
    auth_response_headers:
      - X-Auth-Request-User
      - X-Auth-Request-Email
    add_auth_cookies_to_response:
      - _oauth2_proxy
```

| Field | Default | Description |
| --- | --- | --- |
| `address` | | Required. URL of the auth service. |
| `method` | `GET` | Method of the auth request. `-` uses the method of the original request. |
| `timeout` | `10s` | Timeout of the auth request. |
| `trust_forward_header` | `false` | Keep `X-Forwarded-*`, `X-Original-*`, `X-Real-Ip` and `Forwarded` headers sent by the client. Enable only behind a trusted proxy. |
| `auth_request_headers` | all | Request headers sent to the auth service, e.g. `[Authorization, Cookie]`. |
| `auth_response_headers` | | Headers copied from the auth response to the request sent upstream. |
| `auth_response_headers_prefix` | | Copy every auth response header starting with it, e.g. `X-Auth-`. |
| `add_auth_cookies_to_response` | | Cookies of the auth response set on the client response, e.g. a refreshed session. |
| `forward_body` | `false` | Send the request body to the auth service. |
| `max_body_size` | `1MB` | Largest forwarded body, also for chunked uploads. Larger requests get `413`. |
| `tls.ca` | system roots | PEM CA bundle of the auth service. |
| `tls.cert`, `tls.key` | | PEM client certificate and key. |
| `tls.insecure_skip_verify` | `false` | Skip certificate verification. |

## Headers

The auth service gets these headers describing the original request:

| Header | Value |
| --- | --- |
| `X-Forwarded-Method` | Method, e.g. `POST`. |
| `X-Forwarded-Proto` | `http` or `https`. |
| `X-Forwarded-Host` | Host. |
| `X-Forwarded-Uri` | Path and query. |
| `X-Forwarded-For` | Client IP. |

Without `trust_forward_header`, `X-Forwarded-*`, `X-Original-*`, `X-Real-Ip` and `Forwarded` headers sent by the client are removed and the ones above are set by Turna, so the client cannot fake the request. `X-Forwarded-Proto` is then `https` only when the client connected to Turna with TLS.

Request headers named in `auth_response_headers`, or starting with `auth_response_headers_prefix`, are removed before the auth response headers are copied. A client cannot send `Remote-User: admin` itself.

Redirects of the auth service are not followed; they go to the client, so login pages work. When the auth service cannot be reached the client gets `502`.

The body of an allowing auth response is read and closed before the request continues, so long running requests such as WebSockets do not hold a connection to the auth service.

[`iam_forward_auth`](./iam_forward_auth) is the other direction: it is the endpoint other proxies call to check requests against Turna's IAM.
