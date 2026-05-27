# Environment Values

This example loads a small YAML document into memory and uses it in a service environment variable.

```yaml
log_level: info

loads:
  - name: app_values
    statics:
      - content:
          name: app_values
          content: |
            test:
              message: hello world

print: 'config file: {{ env "CONFIG_FILE" }}'

services:
  - name: show-env
    path: .
    command: env
    inherit_env: true
    env:
      TEST: "1"
      TEST_MESSAGE: "{{ .app_values.test.message }}"
      HOSTTYPE: '{{ env "HOSTTYPE" }}'
    filters:
      - SECRET
```

`inherit_env` copies the current process environment first, then `env` overrides or adds values.
