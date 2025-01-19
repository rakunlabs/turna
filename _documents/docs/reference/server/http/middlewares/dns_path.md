# DNS Path

This middleware is use to reaching scaled services and if they recording IP address in same DNS address than we can use path number and replacement to reach the service.

Example in here we uses `tasks.hello` as DNS address to probably it will return more than one IP address. We can use `(\d+)` to get the number from the path and use it in the replacement.

So `/metrics/hello/1/...` goes to first IP address and `/metrics/hello/2/...` goes to second IP address and go on.

```yaml
server:
  http:
    middlewares:
      dns_path:
        dns_path:
          paths:
            - dns: 'tasks.hello' # DNS name to resolve
              regex: '/metrics/hello/(\d+)/(.*)' # Regex to match the path
              number: '$1' # Number to extract in regex
              replacement: '/$2' # Replacement
              port: 8080 # Port to connect when solve IP address
              insecure_skip_verify: false # Skip verify the certificate
              duration: 10s # Duration to cache the IP address, default is 10s
```

Example configuration:

```yaml
server:
  entrypoints:
    web:
      address: ":8080"
  http:
    middlewares:
      dns_path:
        dns_path:
          paths:
            - dns: 'tasks.hello'
              regex: '/metrics/hello/(\d+)/(.*)'
              number: '$1'
              replacement: '/$2'
              port: 8080
    routers:
      dns:
        path: /*
        middlewares:
          - dns_path
```
