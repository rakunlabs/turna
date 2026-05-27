# role_data

`role_data` returns JSON data selected by roles in the parsed claims.

```yaml
server:
  http:
    middlewares:
      dashboard_data:
        role_data:
          map:
            - roles:
                - admin
              data:
                admin: true
            - roles:
                - auditor
              data:
                read_only: true
          default:
            authenticated: true
```

| Field | Description |
| --- | --- |
| `map[].roles` | Roles that activate this data entry. |
| `map[].data` | Arbitrary JSON/YAML value appended when a role matches. |
| `default` | Value appended to every response when set. Arrays are expanded into the response array. |

`role_data` returns an array. Put `session` before `role_data` so claims are available.
