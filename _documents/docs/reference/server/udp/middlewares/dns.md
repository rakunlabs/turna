# DNS

`dns` answers DNS queries on a UDP entrypoint from a set of statically configured records, falling back to upstream resolvers for names it does not own. Upstreams can be plain DNS, DNS over TLS or DNS over HTTPS, with optional caching, blocklists and per-domain (conditional) forwarding.

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
| `upstream` | | Resolvers queried when no static record matches, tried in order. See [Upstreams](#upstreams). |
| `forward` | | Conditional forwarding rules. See [Conditional forwarding](#conditional-forwarding). |
| `timeout` | `5s` | Timeout for upstream queries. |
| `insecure_skip_verify` | `false` | Skip certificate verification of `tls://` and `https://` upstreams. |
| `cache` | | Cache upstream answers. See [Cache](#cache). |
| `blocklist` | | Block domains. See [Blocklist](#blocklist). |

## Upstreams

| Format | Protocol |
| --- | --- |
| `1.1.1.1:53`, `udp://1.1.1.1:53` | DNS over UDP; truncated answers are retried over TCP. |
| `tcp://1.1.1.1:53` | DNS over TCP. |
| `tls://1.1.1.1:853#cloudflare-dns.com` | DNS over TLS (RFC 7858). The name after `#` is used to verify the certificate; without it the host is used. The port defaults to `853`. |
| `https://cloudflare-dns.com/dns-query` | DNS over HTTPS (RFC 8484, POST). |

```yaml
dns:
  upstream:
    - tls://1.1.1.1:853#cloudflare-dns.com
    - https://dns.google/dns-query
```

## Conditional forwarding

`forward` sends queries of some domains, including their subdomains, to other upstreams. The longest matching domain wins; other names use `upstream`.

```yaml
dns:
  upstream:
    - 1.1.1.1:53
  forward:
    - domains: [corp.local, 10.in-addr.arpa]
      upstream: [10.0.0.2:53]
    - domains: [cluster.local]
      upstream: [tcp://10.96.0.10:53]
```

## Cache

```yaml
dns:
  cache:
    size: 10000
    min_ttl: 0s
    max_ttl: 1h
    negative_ttl: 30s
```

| Field | Default | Description |
| --- | --- | --- |
| `size` | `10000` | Maximum cached answers (LRU). |
| `min_ttl` | `0s` | Minimum cache time. |
| `max_ttl` | `1h` | Maximum cache time. |
| `negative_ttl` | `30s` | Cache time of negative answers without an SOA record. |

Successful and `NXDOMAIN` upstream answers are cached for the lowest record TTL (negative answers use the SOA minimum, RFC 2308). TTLs in cached answers count down. Static records and blocked names are not cached.

## Blocklist

```yaml
dns:
  blocklist:
    domains:
      - ads.example.com
    files:
      - /etc/turna/blocklist.txt
    allow:
      - good.ads.example.com
    response: nxdomain
```

| Field | Default | Description |
| --- | --- | --- |
| `domains` | | Blocked domains; subdomains are blocked too. |
| `files` | | Files with one domain per line or hosts file lines (`0.0.0.0 ads.example.com`). `#` starts a comment; `localhost` entries are ignored. Files are read at startup. |
| `allow` | | Domains (and subdomains) never blocked. |
| `response` | `nxdomain` | `nxdomain`, `refused` or `null_ip` (`0.0.0.0` / `::` for `A`/`AAAA`, empty answer otherwise). |

## Names: `@`, relative, and wildcards

- `@` expands to `origin` (the zone apex). Requires `origin` to be set.
- Relative names (e.g. `www`) get `origin` appended.
- Absolute names must end with a dot (e.g. `host.example.com.`).
- `*` wildcard owner names match per RFC 4592 (closest encloser). A query for `anything.dev.example.com` matches `*.dev` and the answer's owner is the queried name.

## Resolution order

1. Blocked names are answered with the blocklist response.
2. Exact record match for the queried name and type (a `CNAME` is followed when the queried type is not `CNAME`).
3. Wildcard match from the closest encloser upward.
4. If `origin` is set and the name is inside the zone: `NODATA` when the name exists for another type, otherwise `NXDOMAIN`.
5. Otherwise the answer comes from the cache or the query is forwarded to the matching `forward` upstreams or `upstream`.
6. If no upstream is configured, `REFUSED` is returned; if no upstream answers, `SERVFAIL`.

Answers larger than the client buffer (512 bytes or the EDNS0 size) are truncated with the `TC` flag so the client retries over TCP.

Place `dns` last in the router chain; use `ip_allow_list` and `rate_limit` before it to restrict which clients may query.
