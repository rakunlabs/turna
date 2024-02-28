# Session Info

Return the data inside of the token.

```yaml
middlewares:
  session_info:
    session_info:
      information:
        values: [] # Values list to store in the cookie like "preferred_username", "given_name"...
        custom: {} # Custom values to append
        roles: false # If true, it will return roles as []string
        scopes: false # If true, it will return scopes as []string
      session_middleware: "session" # session middleware name to to parse token
```
