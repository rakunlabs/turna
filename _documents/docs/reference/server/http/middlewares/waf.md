# waf

`waf` is a web application firewall built on [Coraza](https://coraza.io). It can load the embedded [OWASP Core Rule Set](https://coreruleset.org) (CRS) and your own SecLang rules.

```yaml
server:
  http:
    middlewares:
      firewall:
        waf:
          crs: true
          paranoia_level: 1
```

| Field | Default | Description |
| --- | --- | --- |
| `mode` | `on` | `on` blocks, `detection_only` only logs matches, `off` disables the middleware. |
| `crs` | `false` | Load the embedded OWASP CRS v4 with the Coraza recommended base configuration. |
| `paranoia_level` | `1` | CRS paranoia level, `1` to `4`. Higher levels catch more and raise more false positives. |
| `inbound_anomaly_threshold` | `5` | CRS anomaly score at which a request is blocked. |
| `outbound_anomaly_threshold` | `4` | CRS anomaly score at which a response is blocked. |
| `directives_files` | | SecLang files loaded after the CRS. Globs are allowed; a pattern matching no file is an error. |
| `directives` | | Inline SecLang directives loaded last, e.g. rule exclusions. |
| `request_body_access` | `true` | Inspect request bodies. |
| `request_body_limit` | `13107200` | Largest request body in bytes. Larger bodies are rejected with `413`. |
| `request_body_in_memory_limit` | | Bytes kept in memory before a body is buffered to a temporary file. |
| `response_body_access` | `false` | Inspect response bodies. The response is buffered until it is checked. |
| `response_body_mime_types` | | Response content types inspected, e.g. `text/html`. |
| `response_body_limit` | | Largest response body inspected, in bytes. |
| `log` | `true` | Log matched rules with rule id, message, client IP, URI and `X-Request-Id`. |

Blocked requests get a JSON error with the status of the rule that denied them, `403` by default. With the CRS, matching rules add to an anomaly score and rule `949110` blocks the request once the score reaches `inbound_anomaly_threshold`.

Building the WAF with the CRS takes a moment at startup and on every reload of the middleware.

## Client IP

Coraza uses the connection address as `REMOTE_ADDR`. Behind a proxy or load balancer, put [`real_ip`](./real_ip) before `waf` so rules and logs see the client address. Put [`request_id`](./request_id) before it to get request ids in the logs.

```yaml
server:
  http:
    routers:
      app:
        path: /*
        middlewares:
          - client_ip
          - request_id
          - firewall
          - backend
```

## Rollout

Start with `mode: detection_only`, check the logs for false positives and add exclusions before switching to `on`.

```yaml
waf:
  crs: true
  mode: detection_only
```

## Custom rules and exclusions

`directives` and `directives_files` take any [SecLang](https://coraza.io/docs/seclang/directives/) directive. They are loaded after the CRS, so `SecRuleRemoveById` and `SecRuleUpdateTargetById` can tune CRS rules.

```yaml
waf:
  crs: true
  directives: |
    # allow HTML in the comment field
    SecRuleUpdateTargetById 941100 "!ARGS:comment"
    # drop a rule that misfires on this application
    SecRuleRemoveById 920350
    # block a legacy path
    SecRule REQUEST_URI "@beginsWith /old-admin" "id:1001,phase:1,deny,status:404,log,msg:'legacy admin'"
  directives_files:
    - /etc/turna/waf/*.conf
```

Custom rule ids must not clash with CRS ids. CRS reserves `900000` to `999999`, and the ids below `200000` used by the base configuration should be avoided too; use ids in the `1` to `99999` range.

Without `crs`, only your own rules run:

```yaml
waf:
  directives: |
    SecRule ARGS:debug "@streq true" "id:1002,phase:1,deny"
```
