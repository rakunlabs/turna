# log

Log the method and path of the request with optional fields.

```yaml
middlewares:
  test:
    log:
      level: info # default is info, string, log level
      message: "" # default is empty, string, add message
      headers: false # default is false, bool, print headers
```
