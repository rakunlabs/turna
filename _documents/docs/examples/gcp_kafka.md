# Kafka OAUTHBEARER With GCP

This example runs Turna as a sidecar next to Redpanda Console. Turna gets a Google Cloud access token through Workload Identity Federation with an X.509 client certificate. Redpanda Console then uses that token for SASL `OAUTHBEARER` on top of mTLS.

```
Redpanda Console ──(token request)──> Turna 127.0.0.1:14293 ──(mTLS)──> Google STS + IAM Credentials
       │
       └──(mTLS + SASL OAUTHBEARER)──> Kafka broker
```

All hostnames, project numbers, and account names below are placeholders. Replace them with your own values.

## Google credentials

Google gives you an `external_account` credential file like this one. Save it, for example as `/etc/certs/gcp/wif-credentials.json`:

```json
{
  "universe_domain": "googleapis.com",
  "type": "external_account",
  "audience": "//iam.googleapis.com/projects/000000000000/locations/global/workloadIdentityPools/my-pool/providers/my-provider",
  "subject_token_type": "urn:ietf:params:oauth:token-type:mtls",
  "token_url": "https://sts.mtls.googleapis.com/v1/token",
  "credential_source": {
    "certificate": { "use_default_certificate_config": true }
  },
  "service_account_impersonation_url": "https://iamcredentials.mtls.googleapis.com/v1/projects/-/serviceAccounts/my-sa@my-project.iam.gserviceaccount.com:generateAccessToken"
}
```

Turna ignores `credential_source`. It uses the certificate from `cert_file` / `key_file` instead. That certificate must be the one registered with the workload identity pool.

## turna.yaml

```yaml
log_level: info

server:
  entrypoints:
    token:
      address: 127.0.0.1:14293   # keep it on localhost
  http:
    middlewares:
      gcp:
        gcp_token:
          format: raw              # use managed_kafka for Google Managed Service for Apache Kafka
          client_id: console
          client_secret: ${GCP_TOKEN_CLIENT_SECRET}
          credentials_file: /etc/certs/gcp/wif-credentials.json
          cert_file: /etc/certs/kafka/client-cert.pem
          key_file: /etc/certs/kafka/client-key.pem
    routers:
      token:
        entrypoints: [token]
        path: /*
        middlewares: [gcp]
```

```sh
export GCP_TOKEN_CLIENT_SECRET='change-me'
turna --config-file turna.yaml
```

Check that a token is returned:

```sh
curl -s -u console:$GCP_TOKEN_CLIENT_SECRET -X POST http://127.0.0.1:14293
```

## Redpanda Console

```yaml
kafka:
  brokers:
    - kafka.example.com:9096
  clientId: my-console
  tls:
    enabled: true
    caFilepath: /etc/certs/kafka/ca.pem
    certFilepath: /etc/certs/kafka/client-cert.pem
    keyFilepath: /etc/certs/kafka/client-key.pem
  sasl:
    enabled: true
    mechanism: OAUTHBEARER
    oauth:
      tokenEndpoint: http://127.0.0.1:14293
      clientId: console
      clientSecret: change-me      # or env KAFKA_SASL_OAUTH_CLIENTSECRET
```

## Kubernetes sidecar

Containers in the same pod share `127.0.0.1`, so both configs above work unchanged:

```yaml
containers:
  - name: console
    image: redpandadata/console:<version>
    env:
      - name: KAFKA_SASL_OAUTH_CLIENTSECRET
        valueFrom: { secretKeyRef: { name: gcp-token, key: secret } }
    volumeMounts:
      - { name: certs, mountPath: /etc/certs, readOnly: true }
  - name: turna
    image: <turna-image>
    args: ["--config-file", "/etc/turna/turna.yaml"]
    env:
      - name: GCP_TOKEN_CLIENT_SECRET
        valueFrom: { secretKeyRef: { name: gcp-token, key: secret } }
    volumeMounts:
      - { name: certs, mountPath: /etc/certs, readOnly: true }
      - { name: turna-config, mountPath: /etc/turna }
```

## Notes

- Certificate files are re-read on every token refresh, so rotated certificates (for example from cert-manager) are picked up without a restart.
- If the broker rejects the token, try switching `format` between `raw` and `managed_kafka`.
- Kafka ACLs usually apply to the service account principal, not to the certificate CN.
- See [`gcp_token`](/reference/server/http/middlewares/gcp_token) for all fields.
