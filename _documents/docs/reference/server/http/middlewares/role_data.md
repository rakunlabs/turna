# Role Data

This middleware return data based on roles.

It gathers data from map if roles exist in token and put in the slice after that return the slice.

```yaml
middlewares:
  role_data:
    role_data:
      map:
      - roles:
        - "admin"
        data: # could be anything
          admin: true
      default: null # default data
```

This example result, if admin role not exist `[]` otherwise:

```json
[
  {
    "admin": true
  }
]
```
