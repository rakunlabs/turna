# ssh_server

`ssh_server` exposes an SSH endpoint as a terminal TCP middleware. It supports public-key authentication, interactive shells, command execution, SFTP, remote forwarding (`ssh -R`), and local forwarding (`ssh -L`/`ssh -D`). Every capability is granted per SSH user.

```yaml
server:
  entrypoints:
    ssh:
      address: ":2222"

  tcp:
    middlewares:
      ssh:
        ssh_server:
          host_keys:
            - /etc/turna/ssh_host_ed25519_key
          banner: "Turna SSH\r\n"
          users:
            - name: deploy
              authorized_key_files:
                - /etc/turna/deploy_authorized_keys

              shell:
                enabled: true
                command: /bin/bash
                os_user: deploy
                allow_exec: true
                env: [LANG, LC_ALL]

              sftp:
                enabled: true
                directory: /srv/deploy
                read_only: false

              forwarding:
                remote: true
                local: false
                allowed_remote_bind:
                  - "127.0.0.1:*"
                  - "0.0.0.0:8000-8999"
                max_remote_forwards: 8
                max_connections: 128

    routers:
      ssh:
        entrypoints: [ssh]
        middlewares: [ssh]
```

Generate a server host key and connect with standard OpenSSH clients:

```sh
ssh-keygen -t ed25519 -f /etc/turna/ssh_host_ed25519_key -N ''

# shell
ssh -p 2222 deploy@example.com

# SFTP
sftp -P 2222 deploy@example.com

# expose local port 3000 on the server's port 8080
ssh -N -p 2222 -R 0.0.0.0:8080:127.0.0.1:3000 deploy@example.com
```

## Configuration

| Field | Description |
| --- | --- |
| `host_keys` | One or more unencrypted SSH private host-key files. Required. |
| `banner` | Optional SSH banner. |
| `users[].name` | SSH login name. |
| `users[].authorized_keys` | Inline OpenSSH public keys. |
| `users[].authorized_key_files` | OpenSSH `authorized_keys` files; multiple keys per file are supported. |
| `users[].shell.enabled` | Permit interactive shell sessions. |
| `users[].shell.command` | Shell executable and optional arguments. Defaults to `/bin/sh` on Unix. |
| `users[].shell.os_user` | Run shell and exec processes as this OS user. Requires permission to change UID/GID. |
| `users[].shell.allow_exec` | Permit `ssh host command`. |
| `users[].shell.env` | Names of client-provided environment variables to accept. |
| `users[].sftp.enabled` | Permit the SFTP subsystem. |
| `users[].sftp.directory` | Working directory for relative paths. This is **not a filesystem jail**. |
| `users[].sftp.read_only` | Reject SFTP write operations. |
| `users[].forwarding.remote` | Permit remote forwarding (`ssh -R`). |
| `users[].forwarding.local` | Permit local and dynamic forwarding (`ssh -L`, `ssh -D`). |
| `users[].forwarding.allowed_remote_bind` | Allowed server-side bind addresses and ports. |
| `users[].forwarding.allowed_local_destination` | Allowed destinations for local/dynamic forwarding. |
| `users[].forwarding.max_remote_forwards` | Listener limit per SSH connection. Defaults to 8. |
| `users[].forwarding.max_connections` | Concurrent forwarded connection limit per SSH connection. Defaults to 128. |

Address rules use `host:port`, `host:*`, or `host:start-end`. IPv6 hosts must use brackets, for example `[::1]:*`. If remote forwarding is enabled without rules, only `localhost`, `127.0.0.1`, and `::1` are allowed. Local forwarding has no default destinations and is denied until rules are configured.

## Security notes

- Password authentication is not supported; use public keys.
- OpenSSH `authorized_keys` options are rejected instead of being silently ignored; express access through the per-user Turna configuration.
- Shell, exec, SFTP, and forwarding are disabled unless explicitly enabled.
- `sftp.directory` only resolves relative paths. Embedded SFTP runs with Turna's OS permissions and absolute paths remain accessible subject to those permissions. Use a dedicated low-privilege Turna process or OS/container isolation when filesystem confinement is required.
- `shell.os_user` changes the shell process UID/GID on Unix, but does not create a chroot.
- Agent forwarding, X11 forwarding, SCP protocol handling, and arbitrary SSH subsystems are not enabled.
