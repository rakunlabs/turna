# role_check

`role_check` authorizes paths and methods against roles parsed by [`session`](./session). It is useful when one router covers many API paths with different role requirements.

```yaml
server:
  http:
    middlewares:
      api_roles:
        role_check:
          allow_others: true
          redirect:
            enable: false
            url: /
          path_map:
            - regex_path: ^/api/transaction/.*
              map:
                - roles:
                    - transaction_r
                    - transaction_rw
                  methods:
                    - GET
                - roles:
                    - transaction_rw
                  write_methods: true
```

## Fields

| Field | Description |
| --- | --- |
| `allow_others` | Allow paths that do not match any `path_map`. |
| `redirect.enable` | Redirect instead of returning JSON `403`. |
| `redirect.url` | Redirect target. |
| `path_map[].regex_path` | Go regex matched against `r.URL.Path`. |
| `path_map[].map` | Authorization rules for the matched path. |

Rule fields:

| Field | Description |
| --- | --- |
| `all_methods` | Apply to every method. |
| `read_methods` | Apply to `GET`, `HEAD`, `OPTIONS`, `TRACE`, and `CONNECT`. |
| `write_methods` | Apply to `POST`, `PUT`, `PATCH`, and `DELETE`. |
| `methods` | Explicit method list. |
| `roles` | Allowed roles. |
| `roles_disabled` | Allow the matching method group without checking roles. |

Put `session` before `role_check` so claims are available.
