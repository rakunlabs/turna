# telemetry

`telemetry` counts datagrams and bytes with the global OpenTelemetry meter provider configured in the top level [`telemetry`](../../../config#telemetry) section, then continues the chain.

```yaml
server:
  udp:
    middlewares:
      metrics:
        telemetry:
          name: dns
    routers:
      dns:
        entrypoints: [dns]
        middlewares: [metrics, resolver]
```

| Field | Default | Description |
| --- | --- | --- |
| `name` | middleware name | Value of the `name` attribute. |

| Metric | Type | Description |
| --- | --- | --- |
| `turna.udp.packets` | counter | Received datagrams. |
| `turna.udp.received` | counter (By) | Received bytes. |

Place it first in the router so dropped datagrams are counted too.
