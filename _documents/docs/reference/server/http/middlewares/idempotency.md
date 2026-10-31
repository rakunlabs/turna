# idempotency

`idempotency` makes `POST` and `PATCH` requests safe to send again. The response of the first request with an `Idempotency-Key` header is stored, and later requests with the same key get that response without reaching the upstream. Clients can retry after a timeout without, for example, charging a card twice.

```yaml
server:
  http:
    middlewares:
      payments_idempotency:
        idempotency:
          required: true
          ttl: 24h
    routers:
      payments:
        path: /payments/*
        middlewares:
          - auth
          - payments_idempotency
          - backend
```

```sh
curl -X POST https://api.example.com/payments \
  -H "Idempotency-Key: 5f1c2a8e-4b7d-4c1e-9f3a-2d6b8e0c7a41" \
  -d '{"amount": 100}'
```

| Field | Default | Description |
| --- | --- | --- |
| `header` | `Idempotency-Key` | Request header holding the key. |
| `methods` | `POST, PATCH` | Methods that use keys. Other requests pass unchanged. |
| `required` | `false` | Reject requests of `methods` without a key with `400`. |
| `ttl` | `24h` | How long a response is stored. |
| `lock_timeout` | `1m` | How long a key stays locked while its first request runs. A request that runs longer may run twice. |
| `scope` | `header:Authorization` | Separates the keys of different clients, so one client cannot read the response of another. Same keys as [`rate_limit`](./rate_limit): `ip`, `header:<name>`, `cookie:<name>`, … |
| `trusted_proxies` | | CIDRs whose forwarding headers are used for the `ip` scope. |
| `statuses` | `200-499` without `408, 409, 425, 429, 499` | Stored statuses, e.g. `[200-299]`. |
| `max_body_size` | `1MB` | Largest request body hashed and response body stored. Larger requests get `413`. |
| `status_header` | `Idempotent-Replayed` | Set to `true` on stored responses. `-` disables it. |
| `max_size` | `64MB` | Size of the memory store. The least recently used keys are removed first. |
| `redis` | | Share keys between instances. Same fields as [`cache` Redis](./cache#redis), `key_prefix` defaults to `turna_idempotency_`. |

## Behavior

| Request | Answer |
| --- | --- |
| New key | Sent upstream; the response is stored when its status is in `statuses`. |
| Same key and request | The stored response with `Idempotent-Replayed: true`. |
| Same key, first request still running | `409 Conflict` with `Retry-After: 1`. |
| Same key, different method, path, query or body | `422 Unprocessable Entity`. |

`5xx` responses and `408`, `409`, `425`, `429` and `499` are not stored by default, so the request can be sent again with the same key and succeed later. A response is also not stored when the client went away before it was complete, since the client may not have seen it.

Replayed responses keep the status, body and headers of the first one, except headers that belong to a single response: `Set-Cookie`, `Date`, `X-Request-Id`, `Traceparent`, `Tracestate` and `Server-Timing`.

Each lock holds a random token. When a request runs longer than `lock_timeout` and another request takes the key, the first one can neither release nor overwrite the second one's lock.

When the store is unavailable, requests go to the upstream without idempotency. With more than one Turna instance use `redis`, otherwise each instance has its own keys.

Put `idempotency` after authentication, so `scope` sees the `Authorization` header and unauthenticated requests do not lock keys.
