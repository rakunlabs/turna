# ldap_view

`ldap_view` is a browser UI for looking at an LDAP directory and testing queries. Each user connects with their own bind: a username and password, an anonymous bind, or an unauthenticated bind. The turna config holds no service account.

```yaml
server:
  http:
    middlewares:
      ldap:
        ldap_view:
          prefix_path: /ldap
          title: Directory
          servers:
            - name: corp
              addr: ldaps://ldap.example.com:636
              base_dn: dc=example,dc=com
              user_dn_template: "uid=%s,ou=people,dc=example,dc=com"
    routers:
      ldap:
        path: /ldap/*
        middlewares:
          - ldap
```

Open `/ldap/`, pick one of the `servers` or type any LDAP URL, choose a bind mode, and connect. Then you can:

- browse the tree from the server's naming contexts and open any entry, with or without operational attributes (`+`);
- find users and groups by name, uid, mail or `sAMAccountName` from the box in the header;
- see the groups an entry belongs to (**Member of**) and the members of a group (**Members**), with nested groups on Active Directory;
- run searches with a base DN, scope (`base`, `one`, `sub`, `children`), filter, attribute list, size and time limits, alias dereferencing, "names only" and paged results;
- pick saved searches that match the server: Active Directory (disabled or locked users, admins, accounts with an SPN or without pre-auth, domain controllers, GPOs, …) or OpenLDAP (POSIX accounts, ppolicy locks, entries changed today);
- format a long filter one clause per line and compact it again (<kbd>Ctrl</kbd>+<kbd>Shift</kbd>+<kbd>F</kbd>);
- view results as entry cards, a table, LDIF or JSON, then filter, copy or download them (LDIF, CSV or JSON);
- follow DN values such as `member` or `manager` to the entry they point to;
- check another user's password with **Test login**, see [Testing a login](#testing-a-login);
- see the root DSE and ask the server who you are bound as (`Who am I`, RFC 4532).

The tree and the entry panel can be resized by dragging their edges, or with the arrow keys on a focused edge. Recent searches and layout are kept in the browser's local storage.

## Readable values

The sparkle button in the header switches between raw and readable values. Readable mode shows:

- generalized times (`createTimestamp`, `whenChanged`, …) and Active Directory FILETIMEs (`pwdLastSet`, `lastLogonTimestamp`, `accountExpires`, …) as local dates, with `0` and the maximum value shown as "never";
- AD intervals such as `maxPwdAge` as durations, and `shadowLastChange` and `shadowExpire` as dates;
- `userAccountControl`, `groupType` and `sAMAccountType` as flag names;
- `objectGUID` as a GUID and `objectSid` as `S-1-…`, with well-known RIDs named.

The raw value stays next to the readable one. Accounts with `ACCOUNTDISABLE` or a ppolicy lock are marked "disabled". Password attributes (`userPassword`, `unicodePwd`, LAPS, Samba hashes) are masked until you click them.

## Testing a login

**Test login** checks a username and password the way an application would, on a new connection to the server of the session. Your session keeps its own bind, and the password is not stored.

- **Search, then bind**: finds the user with a filter such as `(uid=%s)` or `(sAMAccountName=%s)` using your session, then binds as the DN it found. This is how most applications and turna's `auth` middleware log users in. More than one match counts as a failure, as it would in an application.
- **Bind directly**: binds with the username as typed: a DN, `user@domain`, `DOMAIN\user`, or a plain name through `user_dn_template`.

Each step is shown with its result, LDAP result code and time. After a successful bind the user's own entry is read with the user's permissions, which also shows what that user can see. An empty password is refused, because many servers accept it as an unauthenticated bind.

It can also be used to guess passwords, so put an auth middleware in front of the page when it is reachable by people who should not do that.

## Bind modes

| Mode | Sent to the server | Notes |
| --- | --- | --- |
| `simple` | the DN and the password | A plain username is turned into a DN with `user_dn_template`. Usernames that contain `=`, `@` or `\` (a DN, a UPN such as `user@corp.local` or `DOMAIN\user`) are sent unchanged. |
| `anonymous` | an empty DN and an empty password | You see what the server lets anyone read. |
| `unauthenticated` | a DN and an empty password | RFC 4513 5.1.2. Most servers refuse it (OpenLDAP answers `53 Unwilling To Perform`) unless it is turned on. Useful to check that it is off. |

Use `disable_bind_modes` to hide modes on the login screen.

## Sessions

After a successful bind turna keeps that LDAP connection open for the user and sets an `HttpOnly`, `SameSite=Strict` cookie that points to it. The password is not stored anywhere. The connection closes on `Disconnect`, after `idle_timeout`, when the server drops it, or when turna restarts. In each of those cases the UI goes back to the login screen.

Every request runs as the user's own bind, so the server's ACLs decide what is visible. Put an auth middleware in front if only some people should reach the page at all.

## Fields

| Field | Default | Description |
| --- | --- | --- |
| `servers` | | Servers shown on the login screen. |
| `servers[].name` | `addr` | Label on the login screen. |
| `servers[].addr` | | `ldap://`, `ldaps://` or `ldapi://` URL. Required. |
| `servers[].base_dn` | | Default search base. The user can change it. |
| `servers[].start_tls` | `false` | Upgrade an `ldap://` connection with StartTLS. |
| `servers[].insecure_skip_verify` | `false` | Skip TLS certificate checks. Only use it with test servers. |
| `servers[].ca_file` | | PEM file with CA certificates trusted in addition to the system CAs. |
| `servers[].ca` | | Same as `ca_file`, given inline as PEM. |
| `servers[].user_dn_template` | | `fmt` template that turns a username into a bind DN, e.g. `uid=%s,ou=people,dc=example,dc=com`. The username is DN-escaped. It must contain exactly one `%s`. |
| `disable_bind_modes` | | Any of `simple`, `anonymous`, `unauthenticated`. |
| `prefix_path` | | Path prefix the UI is served under. |
| `title` | `LDAP` | UI title. |
| `max_size_limit` | `500` | Upper bound for the entries one search returns. |
| `max_response_size` | `16MB` | Upper bound for the size of one search response in bytes. Entries past it are dropped and the result is marked incomplete. |
| `max_sessions` | `100` | Open sessions, each holding one LDAP connection. When a login needs room, the least recently used session is closed. |
| `max_time_limit` | `30s` | Upper bound for a search, also used as the request timeout. |
| `idle_timeout` | `30m` | Close a session after this long without requests. |
| `dial_timeout` | `10s` | Timeout for connecting to the server. |
| `cookie_name` | `turna_ldap_view` | Session cookie name. |
| `secure_cookie` | `false` | Always set `Secure` on the cookie. It is set anyway when turna terminates TLS. |

`servers` is optional. Users can always type an LDAP URL, with StartTLS and skip-verify options, so turna connects to whatever host they name.
## TLS

| Address | Connection |
| --- | --- |
| `ldaps://host:636` | TLS from the start. |
| `ldap://host:389` with `start_tls` | Plain connection upgraded with StartTLS before the bind. |
| `ldap://host:389` | No encryption; the password travels in clear text. |

Certificates are checked against the system CAs and the host name in the URL. For a server with an internal CA, add that CA with `ca_file` or `ca`. Then you do not have to turn off verification:

```yaml
servers:
  - name: corp
    addr: ldaps://dc01.corp.local:636
    ca_file: /etc/ssl/corp-root-ca.pem
```

For a custom URL the login screen has the same options under **Options**: StartTLS, skip verification, and a CA certificate that can be pasted or chosen from a file. The header shows how the session is protected: `LDAPS`, `StartTLS`, `· custom CA`, or a red `· unverified` when checks are skipped. **Test login** connects the same way as the session.

Common errors:

| Error | Meaning |
| --- | --- |
| `certificate signed by unknown authority` / `certificate is not trusted` | The server's CA is not trusted. Add it with `ca_file` or `ca`. |
| `certificate is valid for …, not …` | The host in the URL is not in the certificate. Use the name the certificate was issued for. |
| `unable to read LDAP response packet: EOF` | `ldap://` was used on a TLS port such as 636. |
| `Network Error: EOF` or `first record does not look like a TLS handshake` | `ldaps://` was used on a plain port such as 389. Use `ldap://` with StartTLS. |

## Large results

A few limits keep big directories from overloading the browser or turna:

- A search returns at most `max_size_limit` entries and `max_response_size` bytes. When either limit is hit, the result is marked incomplete.
- In search results, one attribute shows at most 100 values, followed by "+N more values". A group with 50 000 `member` values therefore stays small in a list. The entry panel loads every value.
- The UI draws 200 entries at a time and the member lists show 1000 at a time, with filters to narrow them.

## Paged results

Some servers cap a single response, Active Directory at 1000 entries for example. Set a page size under **Limits** to use the paged results control (RFC 2696). turna reads page after page until the size limit is reached, then abandons the search on the server.

## API

The UI calls a JSON API under `<prefix>/api/`. POST requests need `Content-Type: application/json`, and cross-site browser requests are refused.

| Method | Path | Description |
| --- | --- | --- |
| `GET` | `info` | Servers, allowed bind modes, limits and the current session. |
| `POST` | `login` | `{server \| addr, mode, username, password, base_dn, start_tls, insecure_skip_verify, ca}` |
| `POST` | `logout` | Close the session. |
| `POST` | `search` | `{base_dn, scope, filter, attributes, size_limit, time_limit, deref, types_only, page_size, max_values}` |
| `GET` | `entry?dn=…[&operational=1]` | Read one entry. |
| `GET` | `children?dn=…` | Direct children for the tree. |
| `GET` | `rootdse` | Root DSE with operational attributes. |
| `GET` | `whoami` | Who am I extended operation. |
| `POST` | `verify` | `{username, password, filter, base_dn, attributes}`; tests a login on a new connection. |

In search results, values that are not printable UTF-8 (`objectGUID`, `jpegPhoto`, …) are base64-encoded, and their attribute has `"binary": true`.

## Trying it locally

```sh
docker run --rm -p 1389:1389 \
  -e LDAP_ADMIN_USERNAME=admin -e LDAP_ADMIN_PASSWORD=adminpw \
  -e LDAP_ROOT=dc=example,dc=org \
  -e LDAP_USERS=alice,bob -e LDAP_PASSWORDS=alicepw,bobpw \
  bitnamilegacy/openldap:latest

make run CONFIG_FILE=testdata/config/ldap-view.yml
```

Open <http://localhost:8080/ldap/>, then log in as `alice` / `alicepw`, anonymously, or as `cn=admin,dc=example,dc=org` / `adminpw`.

The UI source is in `pkg/server/http/middleware/ldapview/_ui` and is built with `pnpm install && pnpm build`. `pnpm check` runs svelte-check with TypeScript 7 (`--tsgo`).
