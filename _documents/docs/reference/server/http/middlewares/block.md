# block

Block the specific http methods.

```yaml
middlewares:
  test:
    block:
      methods: # default is empty, []string
        - GET
        - POST
```
