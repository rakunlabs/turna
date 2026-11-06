# antibot

`antibot` challenges clients before they reach the application, like the BunkerWeb antibot. A browser solves a JavaScript proof of work or a captcha (Cloudflare Turnstile, hCaptcha, Google reCAPTCHA v2) and gets a signed cookie that lets it through until it expires.

```yaml
server:
  http:
    middlewares:
      bot_check:
        antibot:
          type: javascript
          secret: ${ANTIBOT_SECRET}
          difficulty: 16
          cookie:
            max_age: 24h
          skip_paths:
            - /health
            - /api/webhooks/
          allow:
            - 10.0.0.0/8
          trusted_proxies:
            - 172.16.0.0/12
```

With a captcha provider:

```yaml
server:
  http:
    middlewares:
      bot_check:
        antibot:
          type: turnstile
          secret: ${ANTIBOT_SECRET}
          site_key: 0x4AAAAAAA...
          secret_key: ${TURNSTILE_SECRET}
```

| Field | Default | Description |
| --- | --- | --- |
| `type` | `javascript` | `javascript`, `turnstile`, `hcaptcha` or `recaptcha`. |
| `secret` | random | HMAC key (at least 16 bytes) signing cookies and challenges. Without it a random key is generated at start: cookies are lost on restart and not shared between instances. |
| `cookie.name` | `turna_antibot` | Name of the pass cookie. |
| `cookie.max_age` | `24h` | How long a solved challenge is valid. |
| `cookie.secure` | `true` | Secure flag of the cookie. |
| `cookie.same_site` | `lax` | `lax`, `strict` or `none` (requires `secure`). |
| `cookie.domain` | | Cookie domain. |
| `cookie.path` | `/` | Cookie path. |
| `bind_ip` | `true` | Binds the cookie and the challenge to the client IP. |
| `trusted_proxies` | | CIDRs of proxies in front of Turna, see [ip_filter](./ip_filter.md#client-ip). |
| `path` | `/.well-known/turna/antibot` | Challenge endpoint the page posts the solution to. |
| `skip_paths` | | Path prefixes that are not challenged, e.g. health checks and webhooks. The path must match before and after removing `.`/`..` segments, so `/health/../admin` is challenged. |
| `allow` | | IPs or CIDRs that are not challenged. |
| `site_key` | | Public key of the captcha provider. Required for captcha types. |
| `secret_key` | | Server key of the captcha provider. Required for captcha types. |
| `verify_url` | provider URL | Overrides the siteverify URL of the captcha provider. |
| `difficulty` | `16` | Leading zero bits of `sha256(challenge + nonce)` for `javascript`, `1` to `32`. Each bit doubles the work; 16 takes well under a second in a browser. |
| `challenge_ttl` | `5m` | How long a `javascript` challenge can be solved. |
| `challenge_status` | `403` | Status of the challenge page. |
| `status_code` | `403` | Status of the JSON error for requests that cannot see the page. |
| `title` | `Checking your browser` | Title of the challenge page. |
| `message` | | Text of the challenge page. |

## Flow

1. A request without a valid cookie that is `GET` or `HEAD` and accepts `text/html` gets the challenge page with status `challenge_status`. The page remembers the requested URL.
2. Other requests (API calls, `POST`, assets without `text/html`) get `status_code` with a JSON error, so put API routes in `skip_paths` or use another protection for them.
3. The page posts the solution to `path`. For `javascript` the challenge is HMAC-signed with its expiry, difficulty and client IP, so no server state is kept. For captchas the token is checked server-side with the provider `siteverify` endpoint, `secret_key` and the client IP.
4. On success a signed cookie is set and the client is redirected with `303` to the original URL. Only relative paths on the same site are accepted; anything else redirects to `/`.

## Notes

- The pass cookie is `<expiry>.<hmac>`; the IP is only part of the signature. Changing networks (e.g. mobile clients) with `bind_ip` makes the client solve the challenge again.
- The page uses an inline `<script>` and inline `<style>`. With a Content-Security-Policy the challenge page needs `script-src 'unsafe-inline'` (and `style-src 'unsafe-inline'`), and for captchas the provider domains in `script-src` and `frame-src`: `https://challenges.cloudflare.com`, `https://js.hcaptcha.com https://*.hcaptcha.com` or `https://www.google.com https://www.gstatic.com`. Alternatively exclude `path` and the challenged routes from a strict policy.
- The JavaScript proof of work uses `crypto.subtle`, which browsers only expose on secure origins (HTTPS or localhost).
- Search engine crawlers do not solve challenges; add their ranges to `allow` or public pages to `skip_paths` if they should be indexed.
