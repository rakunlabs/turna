# scope

`scope` checks scopes in the parsed claims stored by a previous authentication middleware, usually [`session`](./session).

```yaml
server:
  http:
    middlewares:
      require_write_scope:
        scope:
          scopes:
            - write:transactions
          methods:
            - POST
            - PUT
```

| Field | Default | Description |
| --- | --- | --- |
| `scopes` | | Any one listed scope is enough to pass. Empty means no scope check. |
| `methods` | all | Restrict the check to these HTTP methods. Other methods continue. |
| `noop` | `false` | Disable the check and always continue. |

Put `session` before `scope` in the router chain.
