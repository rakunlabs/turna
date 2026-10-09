# gcp_token

`gcp_token` gets a Google Cloud access token through Workload Identity Federation with an X.509 client certificate (mTLS). If configured, it then impersonates a service account. The token is cached and refreshed before it expires.

Certificates and credentials are given as inline content, so the config does not need any file paths.

There are two modes:

- `endpoint` (default): the middleware responds like an OAuth2 `client_credentials` token endpoint. Use this for clients such as Redpanda Console or librdkafka (`sasl.oauthbearer.token.endpoint.url`).
- `inject`: the middleware sets the token on the request and calls the next middleware, for example a `service` proxy.

```yaml
server:
  entrypoints:
    token:
      address: 127.0.0.1:14293
  http:
    middlewares:
      gcp:
        gcp_token:
          format: raw            # or managed_kafka
          client_id: console
          client_secret: ${GCP_TOKEN_CLIENT_SECRET}
          credentials: |
            {
              "type": "external_account",
              "audience": "//iam.googleapis.com/projects/123/locations/global/workloadIdentityPools/pool/providers/provider",
              "subject_token_type": "urn:ietf:params:oauth:token-type:mtls",
              "token_url": "https://sts.mtls.googleapis.com/v1/token",
              "credential_source": { "certificate": { "use_default_certificate_config": true } },
              "service_account_impersonation_url": "https://iamcredentials.mtls.googleapis.com/v1/projects/-/serviceAccounts/sa@project.iam.gserviceaccount.com:generateAccessToken"
            }
          cert: |
            -----BEGIN CERTIFICATE-----
            ...
            -----END CERTIFICATE-----
          key: |
            -----BEGIN PRIVATE KEY-----
            ...
            -----END PRIVATE KEY-----
    routers:
      token:
        entrypoints: [token]
        path: /*
        middlewares: [gcp]
```

## Fields

Using file paths instead of inline content:

```yaml
gcp_token:
  credentials_file: /opt/certs/gcp/wif-credentials.json
  cert_file: /opt/certs/kafka/certs/kafkaextended-cert.pem
  key_file: /opt/certs/kafka/private/kafkaextended-key.pem
```

| Field | Default | Description |
| --- | --- | --- |
| `credentials` | | Raw `external_account` JSON. `credential_source` is ignored; `cert`/`key` are used instead. |
| `audience` | from `credentials` | Workload identity pool provider audience. |
| `token_url` | `https://sts.mtls.googleapis.com/v1/token` | STS token endpoint. |
| `service_account_impersonation_url` | from `credentials` | If empty, the federated STS token is returned directly. |
| `cert` | | PEM client certificate. It may contain the chain, with the leaf first. |
| `key` | | PEM private key. |
| `ca` | system roots | PEM root CAs for the Google endpoints. |
| `credentials_file` / `cert_file` / `key_file` / `ca_file` | | File path alternatives of the inline fields. A field and its `_file` form cannot both be set. Cert, key and CA files are re-read on each token refresh, so rotated certificates are picked up without a restart. |
| `scopes` | `[https://www.googleapis.com/auth/cloud-platform]` | Requested scopes. |
| `lifetime` | `1h` | Lifetime of the impersonated token. |
| `refresh_before` | `5m` | Refresh the cached token this long before it expires. |
| `timeout` | `30s` | Timeout for upstream calls. |
| `proxy` | proxy env vars | `http`/`https` proxy URL used for the STS and impersonation calls, e.g. `http://user:pass@proxy.local:3128`. mTLS goes through the proxy via `CONNECT`. If empty, `HTTP_PROXY`/`HTTPS_PROXY`/`NO_PROXY` are used. |
| `format` | `raw` | `raw` returns the Google access token. `managed_kafka` wraps it in the JWT-like `GOOG_OAUTH2_TOKEN` format used by Google Managed Service for Apache Kafka. |
| `mode` | `endpoint` | `endpoint` or `inject`. |
| `header` | `Authorization` | Header used in `inject` mode. `Authorization` gets a `Bearer ` prefix. |
| `client_id` / `client_secret` | | Protect the endpoint. Accepted as basic auth or as form values. |

If a refresh fails, the cached token is still served until it actually expires.
