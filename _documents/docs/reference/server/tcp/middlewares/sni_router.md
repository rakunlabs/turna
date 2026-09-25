# sni_router

`sni_router` reads the server name (SNI) of the TLS ClientHello without consuming it and runs the middlewares of the matching route. The TLS stream stays intact, so a route can pass it through encrypted (`redirect`, `load_balancer`) or terminate it (`tls_terminate`).

```yaml
server:
  tcp:
    middlewares:
      router:
        sni_router:
          routes:
            - sni: ["db.example.com"]
              middlewares: [db_passthrough]
            - sni: ["*.apps.example.com"]
              middlewares: [apps_tls, apps_backend]
          default: [web_passthrough]
      db_passthrough:
        redirect:
          address: 10.0.0.5:5432
      apps_tls:
        tls_terminate:
          certificates:
            - cert_file: /certs/apps.crt
              key_file: /certs/apps.key
      apps_backend:
        load_balancer:
          servers:
            - address: 10.0.1.1:8080
            - address: 10.0.1.2:8080
      web_passthrough:
        redirect:
          address: 10.0.0.9:443
    routers:
      tls:
        entrypoints: [https]
        middlewares: [router]
```

| Field | Default | Description |
| --- | --- | --- |
| `routes[].sni` | | Server names. `*.example.com` matches any subdomain, `*` matches all. Case insensitive. |
| `routes[].middlewares` | | TCP middleware names run for the route. |
| `default` | | Middlewares for unmatched names, non-TLS connections and ClientHellos without SNI. When empty those connections are closed. |
| `timeout` | `5s` | Timeout to read the ClientHello. |

Routes are checked in order, the first match wins. `sni_router` is terminal: middlewares after it in the router are not run. Put shared middlewares (`ip_allow_list`, `telemetry`...) before it.
