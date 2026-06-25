# DNS

`dns` answers DNS queries on a UDP entrypoint from a set of statically configured records, falling back to upstream resolvers for names it does not own.

Records use standard zone-file syntax and are parsed with `github.com/miekg/dns`, so zone-file conveniences work.

```yaml
server:
  udp:
    middlewares:
      resolver:
        dns:
          origin: example.com
          ttl: 3600
          records:
            - "@ IN A 10.0.0.1"
            - "www IN A 10.0.0.2"
            - "alias IN CNAME www"
            - "*.dev IN A 10.0.0.9"
          upstream:
            - 1.1.1.1:53
            - 8.8.8.8:53
          timeout: 5s
```

| Field | Default | Description |
| --- | --- | --- |
| `origin` | | Zone name used to expand `@` and relative record names. When set, the responder answers authoritatively (`NXDOMAIN`/`NODATA`) for names inside the zone. |
| `ttl` | `3600` | Default TTL (seconds) applied to records that omit one. |
| `records` | | Zone-file lines. Absolute names need a trailing dot; relative names and `@` resolve against `origin`. |
| `upstream` | | Resolvers (`host:port`) queried when no static record matches. |
| `timeout` | `5s` | Timeout for upstream queries. |

## Names: `@`, relative, and wildcards

- `@` expands to `origin` (the zone apex). Requires `origin` to be set.
- Relative names (e.g. `www`) get `origin` appended.
- Absolute names must end with a dot (e.g. `host.example.com.`).
- `*` wildcard owner names match per RFC 4592 (closest encloser). A query for `anything.dev.example.com` matches `*.dev` and the answer's owner is the queried name.

## Resolution order

1. Exact record match for the queried name and type (a `CNAME` is followed when the queried type is not `CNAME`).
2. Wildcard match from the closest encloser upward.
3. If `origin` is set and the name is inside the zone: `NODATA` when the name exists for another type, otherwise `NXDOMAIN`.
4. Otherwise the query is forwarded to `upstream`.
5. If nothing answers, `REFUSED` is returned.

Place `dns` last in the router chain; use `ip_allow_list` before it to restrict which clients may query.
