# dns_path

`dns_path` resolves a DNS name, selects an IP by a number extracted from the request path, rewrites the path, and proxies to the selected instance.

```yaml
server:
  http:
    middlewares:
      metrics_dns:
        dns_path:
          paths:
            - dns: tasks.hello
              regex: /metrics/hello/(\d+)/(.*)
              number: "$1"
              replacement: "/$2"
              port: 8080
              insecure_skip_verify: false
              duration: 10s
```

In this example, `/metrics/hello/1/...` targets the first IP returned by `tasks.hello`, `/metrics/hello/2/...` targets the second IP, and so on.

Use this middleware for service-discovery setups where several instances share one DNS name but need stable path-based access.
