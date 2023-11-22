# template

Render a template based on respond body, method, etc..

```yaml
middlewares:
  mytemplate:
    template:
      template: |
        This is a template {{ .method }} {{ .path }}
      raw_body: false # default is false, to send body to template as raw or as interface{}
      status_code: 0 # default is 0, to use response status code
      value: "" # from load name, key value and type is map[string]interface{}
      additional: # additional values for the template, default is empty
        key: value
      headers: # additional to return, default is empty
        key: value
      apply_status_codes: # on specific status codes, default is empty
        - 200
        - 201
      trust: false # default is false, to allow to use powerful functions
      work_dir: "" # default is empty, to use current directory
      delims: # default is empty, to use default delimiters
        - "{{"
        - "}}"
```

Values are

```go
"body":         body, // response body []byte or interface{}
"body_raw":     bodyRaw, // response body []byte
"method":       c.Request().Method,
"headers":      c.Request().Header,
"query_params": c.QueryParams(),
"cookies":      c.Cookies(),
"path":         c.Request().URL.Path,
"value":        s.value, // from load name, key value and type is map[string]interface{}
"additional":   s.Additional, // additional values for the template, default is empty
```

Example to mock consul response with folder middleware:

```yaml
server:
  entrypoints:
    web:
      address: ":8080"
  http:
    middlewares:
      health-vault:
        hello:
          message: '[]'
          type: json
      template:
        template:
          template: |
            [
              {
                "CreateIndex": 100,
                "ModifyIndex": 200,
                "LockIndex": 200,
                "Key": "zip",
                "Flags": 0,
                "Value": "{{.body | crypto.Base64B}}",
                "Session": "adf4238a-882b-9ddc-4a9d-5b6758e4159e"
              }
            ]
          raw_body: true
          headers:
            Content-Type: application/json
      consul:
        folder:
          path: ./finops
          browse: false
          spa: false
          index: true
          cache_regex:
            - regex: .*
              cache_control: no-cache
          file_path_regex:
            - regex: "^/v1/kv/finops/(.*)$"
              replacement: "/$1.yaml"
    routers:
      project:
        path: /v1/kv/finops/*
        middlewares:
          - template
          - consul
      health:
        path: /v1/health/service/vault
        middlewares:
          - health-vault
```
