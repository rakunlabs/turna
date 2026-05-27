# role

`role` checks roles in the parsed claims stored by a previous authentication middleware, usually [`session`](./session).

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

| Field | Default | Description |
| --- | --- | --- |
| `roles` | | Any one listed role is enough to pass. Empty means no role check. |
| `methods` | all | Restrict the check to these HTTP methods. Other methods continue. |
| `noop` | `false` | Disable the check and always continue. |

`role` expects claims to implement Turna's role interface. Put `session` before `role` in the router chain.
