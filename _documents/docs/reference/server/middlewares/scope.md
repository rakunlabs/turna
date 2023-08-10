# scope

Check the scopes of the token with specific http methods.

This usable after the `auth` middleware.

```yaml
middlewares:
  test:
    scope:
      methods: # default is empty checking all, []string
        - GET
        - POST
      scopes: # default is empty, []string
        - scope1
        - scope2
```
