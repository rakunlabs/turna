# fault_injection

`fault_injection` delays or fails a share of requests, to test how clients and other services cope with slow and failing dependencies.

```yaml
server:
  http:
    middlewares:
      chaos:
        fault_injection:
          delay:
            duration: 2s
            jitter: 500ms
            percent: 10
          abort:
            status_code: 503
            percent: 5
          header: X-Fault-Inject
          paths:
            - /api
```

| Field | Default | Description |
| --- | --- | --- |
| `delay.duration` | | Time a delayed request waits before it continues. |
| `delay.jitter` | | Random time up to this value added to `duration`. |
| `delay.percent` | | Share of requests delayed, `0` to `100`. |
| `abort.status_code` | `503` | Status of aborted requests. |
| `abort.percent` | | Share of requests aborted, `0` to `100`. |
| `header` | | Only requests with this header get faults. |
| `paths` | | Only these path prefixes get faults. |

At least `delay` or `abort` is required. A request can be both delayed and then aborted.

With `header`, requests without the header or with `false` are not touched. A number between `0` and `100` sets the percent for that request, e.g. `X-Fault-Inject: 100` always fails it. Any other value uses the configured percent.

Aborted responses carry `X-Fault-Injected: abort`.

::: warning
Do not leave `fault_injection` without `header` on production routes.
:::
