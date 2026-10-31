# errors

`errors` replaces error responses with custom pages. The status code stays the same, only the body changes. Pages come from the configuration, a directory or another middleware chain.

```yaml
server:
  http:
    middlewares:
      error_pages:
        errors:
          statuses: [404, 500-599]
          pages:
            "404": |
              <!doctype html>
              <title>Not found</title>
              <h1>{{ .Path }} was not found</h1>
            "500-599": |
              <!doctype html>
              <title>Error</title>
              <h1>{{ .Status }} {{ .StatusText }}</h1>
              <p>Request {{ .RequestID }}</p>
    routers:
      app:
        path: /*
        middlewares:
          - request_id
          - error_pages
          - backend
```

| Field | Default | Description |
| --- | --- | --- |
| `statuses` | | Required. Replaced statuses of `400` and above, e.g. `[404, 500-599]`. Others pass unchanged. |
| `pages` | | Page per status or range, e.g. `"404"` or `"500-599"`. |
| `dir` | | Directory with pages named by status, see below. |
| `content_type` | `text/html; charset=utf-8` | Content type of `pages` and `dir` pages. |
| `service` | | Middleware chain answering pages, e.g. a `service` to an error page server. |
| `query` | `/{status}` | Path requested from `service`; `{status}` is replaced, e.g. `/errors/{status}.html`. |

At least one of `pages`, `dir` or `service` is needed. They are tried in that order. When none has a page for a status, a JSON error is sent.

## Templates

`pages` and `dir` pages are Go templates with:

| Field | Value |
| --- | --- |
| `.Status` | Status code, e.g. `503`. |
| `.StatusText` | Status text, e.g. `Service Unavailable`. |
| `.Method`, `.Path`, `.Host` | Of the request. |
| `.RequestID` | `X-Request-Id` of the request, set by [`request_id`](./request_id). |

With an HTML `content_type` (the default) pages are rendered with `html/template`, so `.Path` and `.Host`, which the client chooses, are escaped. Other content types use `text/template`.

When more than one key of `pages` matches, the narrowest one wins: `404` before `400-499` before `400-599`.

## Directory

`dir` is read at startup. For status `502` the first existing page wins:

1. `502.html`
2. `5xx.html`
3. `error.html`

The extension does not matter, so `404.htm` or `404.txt` work too, but only one file per name. Set `content_type` to match.

## Service

```yaml
error_pages:
  errors:
    statuses: [500-599]
    service: [error_page_server]
    query: /{status}.html
error_page_server:
  service:
    loadbalancer:
      servers:
        - url: http://error-pages:8080
```

The service gets a `GET` with `Accept`, `Accept-Language`, `User-Agent` and `X-Request-Id` of the original request. Its content type is kept. When it does not answer `2xx` with a body of at most 1MB, a JSON error is sent.

Headers of the upstream response such as `WWW-Authenticate`, `Retry-After` and `Set-Cookie` are kept on the error page. Headers describing the replaced body (`Content-Type`, `Content-Length`, `Content-Encoding`, `Content-Disposition`, `ETag`, …) are removed.

`HEAD` requests and WebSocket upgrades pass unchanged. Put `errors` before `compress`, so it sees uncompressed responses.
