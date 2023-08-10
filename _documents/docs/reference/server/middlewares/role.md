# role

Check the role of the token with specific http methods.

This usable after the `auth` middleware.

```yaml
middlewares:
  test:
    role:
      methods: # default is empty checking all, []string
        - GET
        - POST
      roles: # default is empty, []string
        - role1
        - role2
```
