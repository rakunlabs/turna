# reload

`reload` reloads the configuration of Turna when it is called, the same as sending `SIGHUP`. All [config sources](../../../config#application-config-sources) are read again and the `server` section is applied; see [Reload](../../../config#reload).

```yaml
server:
  http:
    middlewares:
      admin_only:
        ip_filter:
          allow:
            - 10.0.0.0/8
      reload_auth:
        basic_auth:
          users:
            - admin:$2y$10$...
      reload:
        reload: {}
    routers:
      reload:
        path: /-/reload
        middlewares:
          - admin_only
          - reload_auth
          - reload
```

```sh
curl -X POST -u admin:secret http://turna:8080/-/reload
```

| Field | Default | Description |
| --- | --- | --- |
| `methods` | `[POST]` | Methods that trigger the reload; other methods get `405`. |
| `timeout` | `1m` | Maximum duration of the reload, including loading remote sources. |

The response is `200 {"message":"configuration reloaded"}` when the new configuration is applied or nothing changed, and `500` with the error when the configuration cannot be loaded or is invalid; the running configuration is kept. Before the server has started the answer is `503`.

`reload` does not call the next middleware. It has no authentication of its own: anyone who reaches the route can reload Turna, so put `ip_filter`, `basic_auth`, `jwt` or a separate internal entrypoint in front of it. The reload finishes even when the client disconnects.
