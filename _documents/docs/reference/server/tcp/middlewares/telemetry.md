# telemetry

`telemetry` records OpenTelemetry metrics of connections with the global meter provider configured in the top level [`telemetry`](../../../config#telemetry) section.

```yaml
telemetry:
  collector: otel-collector:4317

server:
  tcp:
    middlewares:
      metrics:
        telemetry:
          name: postgres
    routers:
      db:
        entrypoints: [db]
        middlewares: [metrics, db_backend]
```

| Field | Default | Description |
| --- | --- | --- |
| `name` | middleware name | Value of the `name` attribute. |

| Metric | Type | Attributes | Description |
| --- | --- | --- | --- |
| `turna.tcp.connections` | counter | `name`, `result` | Handled connections. `result` is `ok`, `error` or `rejected`. |
| `turna.tcp.active_connections` | up-down counter | `name` | Open connections. |
| `turna.tcp.connection.duration` | histogram (s) | `name`, `result` | Connection duration. |
| `turna.tcp.received` | counter (By) | `name` | Bytes received from clients. |
| `turna.tcp.sent` | counter (By) | `name` | Bytes sent to clients. |

Place it first in the router so rejected connections are counted too.
