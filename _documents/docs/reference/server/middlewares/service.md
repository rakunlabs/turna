# service

Service is a middleware that allows to proxy requests to a service.

It is using `RoundRobinBalancer` from `echo` library.

```yaml
middlewares:
  test:
    service:
      loadbalancer:
        servers:
          - url: http://localhost:8080
          - url: http://localhost:8081
```
