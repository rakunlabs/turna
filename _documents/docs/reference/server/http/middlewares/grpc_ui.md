# grpcui

`grpcui` serves a browser UI for a gRPC server.

```yaml
server:
  http:
    middlewares:
      grpcui:
        grpcui:
          addr: dns:///localhost:8080
          basepath: /grpc/
          timer: 5m
    routers:
      grpcui:
        path: /grpc/*
        middlewares:
          - grpcui
```

| Field | Default | Description |
| --- | --- | --- |
| `addr` | | gRPC target address, such as `dns:///localhost:8080`. |
| `basepath` | | Base path where the UI is served. |
| `timer` | `5m` | Duration before closing the backend connection. |

The config key is `grpcui`; the documentation file keeps the older `grpc_ui` path for stable links.
