# tls_terminate

`tls_terminate` performs the TLS handshake and passes the decrypted connection to the next middlewares, for example to forward plain TCP to a backend.

```yaml
server:
  tcp:
    middlewares:
      tls:
        tls_terminate:
          certificates:
            - cert_file: /certs/example.com.crt
              key_file: /certs/example.com.key
          min_version: "1.2"
          alpn: ["http/1.1"]
      postgres:
        redirect:
          address: 10.0.0.5:5432
    routers:
      db:
        entrypoints: [db_tls]
        middlewares: [tls, postgres]
```

| Field | Default | Description |
| --- | --- | --- |
| `certificates` | self-signed | Certificate/key pairs. The certificate is selected by SNI. When empty a self-signed certificate is generated. |
| `min_version` | `1.2` | Minimum TLS version: `1.0`, `1.1`, `1.2` or `1.3`. |
| `alpn` | | ALPN protocols to advertise. |
| `client_ca_file` | | Enables mutual TLS with this CA bundle. |
| `client_auth_optional` | `false` | With `client_ca_file`, verify client certificates only when sent. |
| `handshake_timeout` | `10s` | Handshake timeout. |

Failed handshakes are logged at debug level.
