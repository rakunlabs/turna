# role

`role` authorizes requests with roles from the claims stored by [`session`](./session). The same middleware covers a simple global role requirement, per path/method rules, and returning data selected by roles.

Put `session` before `role` in the router chain.

## Global roles

```yaml
server:
  http:
    middlewares:
      require_admin:
        role:
          roles:
            - admin
          methods:
            - GET
            - POST
```

## Path rules

```yaml
server:
  http:
    middlewares:
      api_roles:
        role:
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

## Role data

When `data` is set, `role` responds with a JSON array instead of calling the next middleware.

```yaml
server:
  http:
    middlewares:
      dashboard_data:
        role:
          data:
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

## Fields

| Field | Default | Description |
| --- | --- | --- |
| `roles` | | Global requirement, any one listed role is enough. Empty means no global check. |
| `methods` | all | Restrict the global `roles` check to these HTTP methods. |
| `path_map[].regex_path` | | Go regex matched against `r.URL.Path`. The first matching entry decides. |
| `path_map[].map` | | Authorization rules for the matched path. |
| `allow_others` | `false` | Allow paths that do not match any `path_map` entry. Only used when `path_map` is set. |
| `redirect.enable` | `false` | Redirect instead of returning a JSON error. |
| `redirect.url` | | Redirect target. |
| `data.map[].roles` | | Roles that activate this data entry. |
| `data.map[].data` | | Arbitrary JSON/YAML value appended when a role matches. |
| `data.default` | | Value appended to every response when set. Arrays are expanded into the response array. |
| `noop` | `false` | Disable the middleware and always continue. |

Path rule fields:

| Field | Description |
| --- | --- |
| `all_methods` | Apply to every method. |
| `read_methods` | Apply to `GET`, `HEAD`, `OPTIONS`, `TRACE`, and `CONNECT`. |
| `write_methods` | Apply to `POST`, `PUT`, `PATCH`, and `DELETE`. |
| `methods` | Explicit method list. |
| `roles` | Allowed roles. |
| `roles_disabled` | Allow the matching method group without checking roles. |

## Working with session

- Roles are read from `realm_access.roles`, `resource_access.*.roles`, and `roles` claims, so bearer tokens, cookie sessions, and API keys from `session` all work.
- On `skip_paths` / `auth_skip_paths`, `session` may pass anonymous requests. `role` then decides by rule: requests that need a role get `401` (or the redirect), while `roles_disabled`, `allow_others`, and `data.default` still work without claims.
- When `session` already allowed a public permission (`auth_skip_paths`), the role check is skipped.
- The `auth_disable_role_check` and `auth_noop` context values (see [`set`](./set)) also skip the check.

A missing role returns `403`; a request without claims returns `401`.
