# hello

Multi purpose middleware that can be used to test the server with return a simple message.

```yaml
middlewares:
  test:
    hello:
      message: "Hello World" # default is empty, string
      status_code: 200 # default is 200, int
      headers: {} # default is empty, map[string]string
      type: string # default is string, it could be json, json-pretty, html, string
      template: false # default is false, bool, use template
      trust: false # default is false, bool, trust of the template dangerous functions
      work_dir: "" # default is empty, string, work_dir for some of the template functions
      delims: # default is empty, delims for the template
        - "{{"
        - "}}"
```

When template is used, values are:

```go
data := map[string]interface{}{
  "body":         body,
  "method":       c.Request().Method,
  "headers":      c.Request().Header,
  "query_params": c.QueryParams(),
  "cookies":      c.Cookies(),
  "path":         c.Request().URL.Path,
}
```
