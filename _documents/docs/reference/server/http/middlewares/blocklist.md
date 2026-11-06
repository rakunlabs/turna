# blocklist

`blocklist` rejects requests by client IP, reverse DNS name, user agent, path or DNSBL listing. Lists can also be loaded from remote URLs and are refreshed periodically.

```yaml
server:
  http:
    middlewares:
      block_bad:
        blocklist:
          deny:
            ip:
              - 203.0.113.0/24
            rdns:
              - .shodan.io
              - .censys.io
            user_agent:
              - (?i)sqlmap|nikto|masscan
            uri:
              - ^/wp-admin
              - \.php$
            urls:
              - url: https://www.spamhaus.org/drop/drop.txt
                type: ip
          allow:
            ip:
              - 10.0.0.0/8
            user_agent:
              - ^kube-probe/
          dnsbl:
            - zen.spamhaus.org
          trusted_proxies:
            - 172.16.0.0/12
```

| Field | Default | Description |
| --- | --- | --- |
| `deny` | | Rules that reject matching requests, see below. |
| `allow` | | Rules whose matches skip every deny and DNSBL check. |
| `dnsbl` | | DNSBL zones; a client listed in any zone is rejected. |
| `refresh` | `1h` | Reload interval of the `urls` lists. |
| `dns_cache` | `1h` | How long reverse DNS and DNSBL results are cached, positive and negative. |
| `dns_timeout` | `2s` | Timeout of a lookup. |
| `dns_cache_size` | `100000` | Maximum number of cached lookups. |
| `rdns_verify` | `true` | Only use reverse DNS names that resolve back to the client IP. |
| `trusted_proxies` | | CIDRs of proxies in front of Turna, see [ip_filter](./ip_filter#client-ip). |
| `status_code` | `403` | Status of rejected requests. |

At least one `deny` rule or `dnsbl` zone is required.

## Rules

`deny` and `allow` take the same fields:

| Field | Description |
| --- | --- |
| `ip` | IPs or CIDRs, IPv4 and IPv6. |
| `rdns` | Domain suffixes of the client reverse DNS name, e.g. `.shodan.io` matches `scanner.shodan.io` and `shodan.io`. |
| `user_agent` | Regular expressions matched against the `User-Agent` header. |
| `uri` | Regular expressions matched against the request path. |
| `urls` | Remote lists, each with `url` and `type` (`ip`, `rdns`, `user_agent` or `uri`). |

Regular expressions are not anchored, use `^` and `$` to match the whole value. `uri` rules see both the raw path and the path with `.`/`..` segments and repeated slashes removed: a `deny` rule matches if either form matches, an `allow` rule only if both match, so `/public/../admin` is not allowed by `^/public/`.

## URL lists

`url` is an `http(s)://` address, a `file://` URL or a local path. The file has one entry per line; blank lines and lines starting with `#` are ignored. For `ip` and `rdns` lists only the first word is used, so trailing comments like `1.2.3.0/24 ; SBL123` work. Invalid entries are skipped with a warning.

Lists are loaded at startup and every `refresh`. When a load fails the error is logged and the previously loaded list is kept.

## DNS

Reverse DNS and DNSBL lookups are only done when `rdns` rules (including `rdns` URL lists) or `dnsbl` zones are configured. Within `allow` and `deny` the reverse DNS rules are checked last, and DNSBL zones only after the deny rules did not match. Results are cached for `dns_cache`; failures other than "not found" are cached for at most a minute.

With `rdns_verify` every PTR name is resolved again and must contain the client IP, so a client cannot pass a forged reverse DNS name.

DNSBL queries use the reversed IPv4 octets (`1.2.0.192.zen.spamhaus.org`) or reversed IPv6 nibbles; an `A` answer in `127.0.0.0/8` means the client is listed. Answers in `127.255.255.0/24` (Spamhaus error codes, e.g. for queries from public resolvers) and outside `127.0.0.0/8` (resolvers rewriting NXDOMAIN) are logged as warnings and not treated as listings. Public DNSBLs may refuse queries from public resolvers, use a local resolver.
