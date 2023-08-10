# set

Set values to the context. It is useful for interacting other middlewares.

```yaml
middlewares:
  test:
    set:
      values: # default is empty, []string values always true
        - value1
        - value2
      map: # default is empty, map[string]interface{}
        key: value
```
