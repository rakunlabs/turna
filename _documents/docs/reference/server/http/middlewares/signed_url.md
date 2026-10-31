# signed_url

`signed_url` lets requests pass when they carry a valid, unexpired signature in the query, like nginx `secure_link`. Links can be shared without a login and stop working when they expire, e.g. for downloads.

```yaml
server:
  http:
    middlewares:
      strip_files:
        strip_prefix:
          prefix: /files
      signed:
        signed_url:
          secrets:
            - change-me-to-a-long-random-secret
          max_ttl: 168h
          sign_path: /_sign
      downloads:
        folder:
          path: /srv/downloads
    routers:
      sign:
        path: /files/_sign
        middlewares: [auth, strip_files, signed]
      files:
        path: /files/*
        middlewares: [strip_files, signed, downloads]
```

The `sign` router puts `auth` in front of `sign_path`, so only logged in users can create links. The `files` router has no auth; the signature is the access check.

A signed link looks like:

```txt
https://example.com/files/report.pdf?expires=1767225600&signature=Xc3l0q...
```

| Field | Default | Description |
| --- | --- | --- |
| `secrets` | | Required. Signing secrets of at least 16 bytes. The first signs, any of them verifies, so a new secret can be added before the old one is removed. |
| `expires_param` | `expires` | Query parameter with the expiry as unix seconds. |
| `signature_param` | `signature` | Query parameter with the signature. |
| `sign_query` | `true` | Sign the other query parameters too, so they cannot be changed or added. |
| `strip_params` | `true` | Remove `expires` and `signature` from the query sent upstream. |
| `path_prefix` | `false` | A link signed for a path ending in `/` opens everything below it. |
| `max_ttl` | | Reject links that expire further in the future. No limit when empty. |
| `sign_path` | | Path answering signed links, see below. Protect it with an auth middleware. |
| `default_ttl` | `1h` | Lifetime of links from `sign_path`. |
| `status_code` | `403` | Status of rejected requests. |

## Signing links

With `sign_path`, a request to it answers a signed link of the `path` query parameter:

```sh
curl "https://example.com/files/_sign?path=/report.pdf&ttl=24h" -H "Authorization: Bearer ..."
```

```json
{ "url": "/report.pdf?expires=1767225600&signature=Xc3l0q...", "expires": 1767225600 }
```

The returned path is the one `signed_url` sees, after middlewares before it like `strip_prefix`. Add the prefix back when sharing the link.

Links can also be signed by an application. The signature is the unpadded base64url HMAC-SHA256 of:

```txt
<expires>\n<path>\n<query without expires and signature, sorted by key>
```

With `sign_query: false` the last line is empty. With `path_prefix`, `<path>` is the signed prefix, e.g. `/share/folder/`.

```sh
secret='change-me-to-a-long-random-secret'
path='/report.pdf'
expires=$(( $(date +%s) + 3600 ))
signature=$(printf '%s\n%s\n' "$expires" "$path" | openssl dgst -sha256 -hmac "$secret" -binary | basenc --base64url | tr -d '=')
echo "$path?expires=$expires&signature=$signature"
```

```go
func sign(secret, path string, query url.Values, ttl time.Duration) string {
	expires := strconv.FormatInt(time.Now().Add(ttl).Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(expires + "\n" + path + "\n" + query.Encode()))

	query.Set("expires", expires)
	query.Set("signature", base64.RawURLEncoding.EncodeToString(mac.Sum(nil)))

	return path + "?" + query.Encode()
}
```

The host is not signed, so one link works on every host of the router.

Paths are cleaned before they are checked: `.` and `..` segments are resolved and repeated slashes are merged, the same way `folder` and `file_manager` resolve them. A link for `/share/folder/` therefore cannot open `/share/folder/../private/secret.txt`. The upstream receives the cleaned path.
