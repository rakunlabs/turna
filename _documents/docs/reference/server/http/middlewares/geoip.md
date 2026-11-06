# geoip

`geoip` allows or denies requests by the country and autonomous system (ASN) of the client IP. It reads MaxMind GeoLite2/GeoIP2 or DB-IP `mmdb` files and can pass the country and ASN to the upstream as request headers.

```yaml
server:
  http:
    middlewares:
      geo:
        geoip:
          country_db: /data/GeoLite2-Country.mmdb
          asn_db: /data/GeoLite2-ASN.mmdb
          allow_countries: [TR, DE, NL]
          deny_asn: [14061, 16509]
          unknown: allow
          country_header: X-Country-Code
          asn_header: X-ASN
          reload_interval: 1h
          trusted_proxies:
            - 172.16.0.0/12
```

| Field | Default | Description |
| --- | --- | --- |
| `country_db` | | Path of a country or city database, e.g. `GeoLite2-Country.mmdb` or `GeoLite2-City.mmdb`. Required for country rules and `country_header`. |
| `asn_db` | | Path of an ASN database, e.g. `GeoLite2-ASN.mmdb`. Required for ASN rules and `asn_header`. |
| `allow_countries` | | ISO 3166-1 alpha-2 codes, case-insensitive. When set, only matching clients pass. |
| `deny_countries` | | ISO codes that are rejected. |
| `allow_asn` | | AS numbers. When set, only matching clients pass. |
| `deny_asn` | | AS numbers that are rejected. |
| `unknown` | `allow` | `allow` or `deny` clients without a record, e.g. private ranges, unix sockets or IPs missing in the database. |
| `country_header` | | Request header set to the country code for the upstream. Empty does not set it. |
| `asn_header` | | Request header set to the AS number for the upstream. Empty does not set it. |
| `reload_interval` | `0` | Checks the database files for changes and reopens them. `0` disables reloading. |
| `trusted_proxies` | | CIDRs of proxies in front of Turna, see [ip_filter](./ip_filter.md#client-ip). |
| `status_code` | `403` | Status of rejected requests. |

At least one allow, deny or header entry is required.

## Notes

- Deny rules are checked first and win over allow rules. Country and ASN rules both apply: a client must pass each of them.
- The country is taken from `country`, and from `registered_country` when the record has no `country` (e.g. some anycast ranges).
- Incoming `country_header` and `asn_header` values are always removed, so clients cannot forge them. They are set only when a record is found.
- Databases are not downloaded by Turna. Update them with `geoipupdate` or a cron job that writes a new file and renames it over the old one; with `reload_interval` set, the change of modification time or size is picked up, the new reader is swapped in atomically and the old one is closed after running lookups finish. A file that fails to open keeps the old database. Do not rewrite the file in place, it is memory-mapped.
