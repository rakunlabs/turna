# gRPC UI

Show gRPC UI in the browser to reaching your services.

```yaml
server:
  http:
    middlewares:
      grpcui:
        grpcui:
            addr: dns:///localhost:8080 # addr is the address of the gRPC server like 'dns:///localhost:8080'
            basepath: /xyz/
            timer: "5m" # default is 5m, duration, timer to close the backend connection
```

Example configuration:

```yaml
server:
  entrypoints:
    web:
      address: ":8082"
  http:
    middlewares:
      grpcui:
        grpcui:
          addr: dns:///localhost:8080
          basepath: /xyz/
    routers:
      project:
        path: /xyz/*
        middlewares:
          - grpcui
```
