# view

`view` serves a combined UI for Swagger definitions, gRPC services, iframes,
and reverse-proxied pages.

```yaml
server:
  http:
    middlewares:
      docs_view:
        view:
          prefix_path: /view/
          info_url_type: yaml
          info:
            swagger_settings:
              base_path_prefix: /api
              disable_authorize_button: true
              schemes: [HTTPS]
            swagger:
              - name: petstore
                link: https://petstore.swagger.io/v2/swagger.json
            page:
              - name: admin
                path: admin
                url: http://admin.internal:8080
                rewrite:
                  base: true
                  absolute: true
                  origin: true
                  location: true
                  cookie: true
                  frame: true
                  forward_prefix: true
                  replace:
                    - old: https://another.internal
                      new: "{{prefix}}"
```

| Field | Description |
| --- | --- |
| `prefix_path` | Base path for the UI. |
| `info_url` | Optional URL that returns the Swagger list. |
| `info_url_type` | `yaml` or `json` when `info_url` is used. |
| `insecure_skip_verify` | Skip TLS verification while fetching `info_url`. |
| `info.swagger` | List of Swagger documents. |
| `info.swagger_settings` | Defaults applied to Swagger documents. |
| `info.page` | Pages reverse-proxied under `{prefix_path}/page/{path}`. |

Use `info_url` when the list should come from another service; otherwise configure `info` inline.

## Page rewriting

Page rewriting is opt-in. It adapts an application that expects to run at the
root so it can run under the view page prefix.

| Field | Description |
| --- | --- |
| `base` | Inject a `<base>` tag into HTML so relative references resolve under the page prefix. |
| `absolute` | Prefix root-absolute HTML and CSS references such as `/assets/app.js`. Protocol-relative URLs are unchanged. |
| `origin` | Replace references to the configured backend origin with the page prefix, including JSON-escaped origins. |
| `location` | Rewrite same-backend absolute and root-relative `Location` headers. |
| `cookie` | Prefix `Set-Cookie` paths and remove backend `Domain` attributes. |
| `frame` | Remove `X-Frame-Options` and only the CSP `frame-ancestors` directive. |
| `forward_prefix` | Send the public page path in `X-Forwarded-Prefix`. |
| `content_types` | Media types whose bodies may be rewritten. Defaults to HTML, XHTML, and CSS. |
| `max_body_size` | Maximum response body buffered for rewriting, in bytes. Defaults to 10 MiB. Larger responses pass through unchanged. |
| `replace` | Custom literal or RE2 regular expression body replacements, applied after built-in rewriting. |

Each `replace` entry accepts `old` or `regex`, `new`, and optional
`content_types`. `new` supports `{{prefix}}` and `{{url}}`; regular expression
replacements also support `$1` capture expansion.

When body rewriting is enabled, the proxy requests identity encoding from the
backend. Gzip responses are also decoded safely when a backend ignores that
request. Other content encodings pass through unchanged.
