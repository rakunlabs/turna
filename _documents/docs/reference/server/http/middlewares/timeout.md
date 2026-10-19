# timeout

`timeout` bounds the time spent on a request. A request that does not start its response in time gets `504 Gateway Timeout`.

```yaml
server:
  http:
    middlewares:
      request_timeout:
        timeout:
          duration: 30s
          rules:
            - prefix:
                - /export
                - /reports
              duration: 5m
            - prefix: /events
              duration: 0
```

| Field | Description |
| --- | --- |
| `duration` | Default timeout. `0` disables it for paths not matched by a rule. |
| `rules[].prefix` | Path prefix, or a list of them. |
| `rules[].duration` | Timeout of matching paths, `0` disables it. |

Rules are checked in order and the first match wins. At least `duration` or one rule is required.

The deadline is set on the request context, so `service`, `request` and other middlewares stop waiting for the upstream. If the response has already started (for example a stream), it is cut when the deadline passes instead of being replaced.

WebSocket upgrades are not limited. Use `duration: 0` rules for SSE and other long-lived streams.

gRPC requests get a `DEADLINE_EXCEEDED` status instead of `504`.

For upstream connection limits that do not include reading the body, see `timeouts` in [`service`](./service).
