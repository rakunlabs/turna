# iam

`iam` serves Turna's embedded identity and access management API/UI. It stores users, service accounts, roles, permissions, LDAP mappings, and access-check data.

> Deprecated: use [`auth`](./auth) for new PostgreSQL-backed IAM/OAuth2 setups. `iam` remains available for existing Badger-backed deployments during migration.

```yaml
server:
  http:
    middlewares:
      iam:
        iam:
          prefix_path: /iam
          database:
            path: ./data/iam
            memory: false
            flatten: true
            backup_path: ""
            write_api: ""
            pubsub_topic: turna-iam
            redis:
              addrs:
                - localhost:6379
          check:
            default_hosts:
              - example.com
            no_host_check: false
```

## Fields

| Field | Description |
| --- | --- |
| `prefix_path` | Base path for IAM API, UI, and Swagger. It is normalized to start with `/`. |
| `database.path` | Badger database path. Required unless `database.memory` is true. |
| `database.memory` | Use in-memory storage. |
| `database.backup_path` | Restore database from a backup on startup. |
| `database.flatten` | Flatten inherited role/permission data on startup. |
| `database.write_api` | Read-only mode: sync from another IAM service's API. |
| `database.redis` | Redis connection used for IAM synchronization. |
| `database.pubsub_topic` | Redis Pub/Sub topic for sync notifications. |
| `ldap` | Optional LDAP configuration for user/group sync. |
| `check.default_hosts` | Hosts used when permission resources do not define hosts. |
| `check.no_host_check` | Disable host checks in permission evaluation. |

## Main Routes

With `prefix_path: /iam`, the middleware exposes:

| Route | Purpose |
| --- | --- |
| `/iam/ui/*` | Embedded IAM UI. |
| `/iam/swagger/*` | Embedded Swagger files. |
| `/iam/v1/users` | User API. |
| `/iam/v1/service-accounts` | Service account API. |
| `/iam/v1/roles` | Role API. |
| `/iam/v1/permissions` | Permission API. |
| `/iam/v1/check` | Permission check API used by `iam_check`. |
| `/iam/check` | User-facing check API. |
| `/iam/v1/backup`, `/iam/v1/restore`, `/iam/v1/sync` | Backup and synchronization APIs. |

`iam` registers itself by middleware name, which allows [`oauth2`](./oauth2) and [`iam_forward_auth`](./iam_forward_auth) to use it directly.
