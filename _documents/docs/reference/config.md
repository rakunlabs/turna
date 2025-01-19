# Config

```yaml
log_level: info # application log, default is info

print: "text to print after the load complate: {{ .APP_NAME }}"

loads: [] # check loads section
preprocess: [] # check preprocess section
server: {} # check server section
services: [] # check services section
```

Turna now has 4 main sections, `loads`, `preprocess`, `server` and `services`. Check their sections for more information.

- [loads](loads.md)
- [preprocess](preprocess/preprocess.md)
- [server](server/server.md)
- [services](services.md)

Inside of the main parameters, there is `--config-consul`, `--config-vault` additional parameters to get configuration from consul and vault (consul first).  
Also this can editable with environment variables.

```sh
APP_NAME=test # default is turna
PREFIX_VAULT=finops # default is empty
PREFIX_CONSUL=finops # default is empty

# First initialize configuration, these variables are default
CONFIG_SET_CONSUL=false
CONFIG_SET_VAULT=false
CONFIG_SET_FILE=true
```

To set Consul's and Vault's configuration use their environment variables.

For basic configuration:

```sh
# CONSUL
CONSUL_HTTP_ADDR="localhost:8500"

# VAULT
VAULT_ADDR="http://localhost:8200"
VAULT_ROLE_ID="${ROLE_ID}"
# VAULT_CONSUL_ADDR_DISABLE=false
```

## Environment variables

### Consul

Using `github.com/hashicorp/consul/api`

| Environment variable   | Description            |
| ---------------------- | ---------------------- |
| CONSUL_HTTP_ADDR       | Consul address         |
| CONSUL_HTTP_TOKEN_FILE | Consul token file      |
| CONSUL_HTTP_TOKEN      | Consul token           |
| CONSUL_HTTP_AUTH       | Consul auth            |
| CONSUL_HTTP_SSL        | Consul ssl             |
| CONSUL_TLS_SERVER_NAME | Consul tls server name |
| CONSUL_CACERT          | Consul cacert          |
| CONSUL_CAPATH          | Consul capath          |
| CONSUL_CLIENT_CERT     | Consul client cert     |
| CONSUL_CLIENT_KEY      | Consul client key      |
| CONSUL_HTTP_SSL_VERIFY | Consul ssl verify      |
| CONSUL_NAMESPACE       | Consul namespace       |
| CONSUL_PARTITION       | Consul partition       |

### Vault

Using `github.com/hashicorp/vault/api`

| Environment variable    | Description          |
| ----------------------- | -------------------- |
| VAULT_ROLE_ID           | Role ID              |
| VAULT_ROLE_SECRET       | Role secret          |
| VAULT_ADDR              | Vault address        |
| VAULT_TOKEN             | Vault token          |
| VAULT_AGENT_ADDR        | Vault agent address  |
| VAULT_MAX_RETRIES       | Max retries          |
| VAULT_CACERT            | CA certificate       |
| VAULT_CACERT_BYTES      | CA certificate bytes |
| VAULT_CAPATH            | CA path              |
| VAULT_CLIENT_CERT       | Client certificate   |
| VAULT_CLIENT_KEY        | Client key           |
| VAULT_RATE_LIMIT        | Rate limit           |
| VAULT_CLIENT_TIMEOUT    | Client timeout       |
| VAULT_SKIP_VERIFY       | Skip verify          |
| VAULT_SRV_LOOKUP        | SRV lookup           |
| VAULT_TLS_SERVER_NAME   | TLS server name      |
| VAULT_HTTP_PROXY        | HTTP proxy           |
| VAULT_PROXY_ADDR        | Proxy address        |
| VAULT_DISABLE_REDIRECTS | Disable redirects    |
| VAULT_APPROLE_BASE_PATH | /auth/approle/login/ |

## File

Giving a file configuration, config file can be __[toml, yaml, yml, json]__ in the same directory with binary (`turna.yaml`) or your `APP_NAME` env value so if it is `test` than files checking `test.[...]`.  
`CONFIG_FILE` environment variable also can be used to set config file name. Codec understand the file type from the extension.
