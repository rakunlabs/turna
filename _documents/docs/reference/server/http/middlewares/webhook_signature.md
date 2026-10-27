# webhook_signature

`webhook_signature` verifies the HMAC signature of webhook requests and rejects unsigned or tampered ones with `401 Unauthorized`. The body is passed on unchanged.

```yaml
server:
  http:
    middlewares:
      github_webhook:
        webhook_signature:
          preset: github
          secrets:
            - ${GITHUB_WEBHOOK_SECRET}
      partner_webhook:
        webhook_signature:
          header: X-Signature
          prefix: "sha256="
          timestamp_header: X-Timestamp
          secrets:
            - ${PARTNER_SECRET}
```

| Field | Default | Description |
| --- | --- | --- |
| `preset` | `generic` | `github`, `stripe`, `slack`, `shopify`, `standard` or `generic`. |
| `secrets` | | Required. Signing secrets. Any of them may match, so a new secret can be added before the old one is removed. |
| `secret_encoding` | `raw` | `raw`, `hex` or `base64`. |
| `header` | from preset | Header holding the signature. Required for `generic`. |
| `algorithm` | `sha256` | `sha1`, `sha256` or `sha512`. |
| `encoding` | `hex` | Signature encoding, `hex` or `base64`. |
| `prefix` | | Text before the signature, e.g. `sha256=`. |
| `timestamp_header` | | Header with the request time in Unix seconds. It is signed as `<timestamp>.<body>`. |
| `tolerance` | `5m` | Accepted age of the timestamp, against replays. |
| `max_body_size` | `1MB` | Largest body read. Larger bodies get `413`. |
| `status_code` | `401` | Status of rejected requests. |

## Presets

| Preset | Header | Signed payload |
| --- | --- | --- |
| `github` | `X-Hub-Signature-256: sha256=<hex>` | body |
| `shopify` | `X-Shopify-Hmac-Sha256: <base64>` | body |
| `slack` | `X-Slack-Signature: v0=<hex>`, `X-Slack-Request-Timestamp` | `v0:<timestamp>:<body>` |
| `stripe` | `Stripe-Signature: t=<timestamp>,v1=<hex>` | `<timestamp>.<body>` |
| `standard` | `Webhook-Signature: v1,<base64>`, `Webhook-Id`, `Webhook-Timestamp` | `<id>.<timestamp>.<body>` |

[Standard Webhooks](https://www.standardwebhooks.com/) is also used by Svix and others. Its secrets are base64 with an optional `whsec_` prefix. Fields set next to a preset override it.

Signatures are compared in constant time.
