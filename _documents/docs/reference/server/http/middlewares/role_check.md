# Role Check

This middleware works after the session middleware and checks the user's role to block or allow access to the requested path.

```yaml
middlewares:
  role_check:
    role_check:
      allow_others: true
      path_map:
      - regex_path: "^/api/transaction/.*"
        map:
          - roles:
            - "transaction_r"
            - "transaction_rw"
            methods:
            - "GET"
          - roles:
            - "transaction_rw"
            methods:
            - "POST"
            - "PUT"
            - "DELETE"
```

> Check login example.
