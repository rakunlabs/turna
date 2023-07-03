![turna](_assets/turna.svg#gh-light-mode-only)
![turna](_assets/turna_light.svg#gh-dark-mode-only)

[![License](https://img.shields.io/github/license/worldline-go/turna?color=blue&style=flat-square)](https://raw.githubusercontent.com/worldline-go/turna/main/LICENSE)
[![Coverage](https://img.shields.io/sonar/coverage/worldline-go_turna?logo=sonarcloud&server=https%3A%2F%2Fsonarcloud.io&style=flat-square)](https://sonarcloud.io/summary/overall?id=worldline-go_turna)
[![GitHub Workflow Status](https://img.shields.io/github/actions/workflow/status/worldline-go/turna/test.yml?branch=main&logo=github&style=flat-square&label=ci)](https://github.com/worldline-go/turna/actions)
[![Go Report Card](https://goreportcard.com/badge/github.com/worldline-go/turna?style=flat-square)](https://goreportcard.com/report/github.com/worldline-go/turna)

Turna gets configuration files from various sources and runs commands.

With _turna_, we can use third party programs directly in our systems without giving extra configuration files to them.

## Installation

Check the releases page for versions and download the binary for your system.

```sh
curl -fsSL https://github.com/worldline-go/turna/releases/latest/download/turna_Linux_x86_64.tar.gz | sudo tar -xz --overwrite -C /usr/local/bin/ turna
```

## Usage

Give config file with `CONFIG_FILE` env value [toml, yaml, yml, json] extensions supported.

To get this file from consul and vault area set the consul and vault enviroment variables.

```sh
# APPNAME
APP_NAME=test
PREFIX_VAULT=finops
PREFIX_CONSUL=finops

# First initialize configuration, these variables are default
CONFIG_SET_CONSUL=false
CONFIG_SET_VAULT=false
CONFIG_SET_FILE=true

# CONSUL
CONSUL_HTTP_ADDR="localhost:8500"
# VAULT
VAULT_ADDR="http://localhost:8200"
VAULT_ROLE_ID="${ROLE_ID}"
# VAULT_CONSUL_ADDR_DISABLE=false
```

## Configuration

```yml
# application log, default is info
log_level: info

# loads configuration to files
loads:
  - export: test.yml
    # file_perm: "0644"
    # folder_perm: "0755"
    # name using to export value in map
    name: mytest
    # static configuration merged with other sources
    statics:
      - consul:
          # name using to export value in map
          name: myconsul
          path: test
          # default is empty
          path_prefix: finops
          # load as raw
          raw: false
          # default is YAML, [toml, yaml, json] supported
          codec: "YAML"
          # get the inner path
          inner_path: "test"
          # remap key
          map: "myapp/inner"
          template: false
          # decode base64
          base64: false
        vault:
          # name using to export value in map
          name: myvault
          path: test/myapp
          # default is empty, path_prefix is must!
          path_prefix: secret
          # default is auth/approle/login, not need to set
          app_role_base_path: auth/approle/login
          # get the inner path
          inner_path: "test"
          # remap key
          map: "myapp/inner"
          template: false
          # decode base64
          base64: false
        file:
          # name using to export value in map
          name: myfile
          # default is empty, [toml, yml, yaml, json] supported
          path: load.yml
          raw: false
          # get the inner path
          inner_path: "test"
          # remap key
          map: "myapp/inner"
          template: false
          # decode base64
          base64: false
        content:
          # name using to export value in map
          name: mycontent
          codec: "YAML"
          content: |
            test:
              test: 1
              test2: 2
          raw: false
          template: false
          # get the inner path
          inner_path: "test"
          # remap key
          map: "myapp/inner"
          # decode base64
          base64: false
    dynamics:
      - consul:
          # name using to export value in map
          name: myconsulDynamic
          path: test
          # default is empty
          path_prefix: finops
          # load as raw
          raw: false
          # default is YAML, [toml, yaml, json] supported
          codec: "YAML"
          # get the inner path
          inner_path: "test"
          # remap key
          map: "myapp/inner"
          template: false

print: "text to print when run this application to add logs, after the load complate: {{ .APP_NAME }}"

server:
  # load_value: "x-server"
  entrypoints:
    web:
      address: ":8080"
  http:
    middlewares:
      test:
        addprefix:
          prefix: /test
      auth:
        auth:
          redirect:
            # cookie_name: "test_1234"
            # max_age: 3600
            # path: "/"
            # callback: "/login/"
            # base_url: "http://localhost:8000/"
            # NoClientIDParam is use to not add client_id in request of code.
            no_client_id_param: false
            # Schema is the default schema to use for the redirect if no schema is provided.
            schema: "https"
            # Secure is the secure flag for the cookie.
            secure: false
            # User-Agent to check if the request is a browser.
            check_agent: false
            # CheckValue is the value to check in the context (combined with other middlewares).
            check_value: ""
            # TokenHeader for set to the header of the token.
            token_header: false
            # refresh_token is use to refresh the token if it is expired.
            refresh_token: false
          provider:
            keycloak:
              base_url: "http://localhost:8080/"
              realm: "master"
              client_id: "test"
              client_secret: "my_client_secret"
              scopes:
                - openid
                - email
                - profile
            generic:
              client_id: oktaID
              client_secret: oktaSecret
              cert_url: https://example.com/oauth2/v1/keys
              auth_url: https://example.com/oauth2/v1/authorize
              token_url: https://example.com/oauth2/v1/token
              # if introspect_url exist then cert_url not usable for validate the token
              introspect_url: https://example.com/oauth2/v1/introspect
              scopes:
                - openid
                - email
                - profile
      service:
        service:
          loadbalancer:
            servers:
              - url: "http://localhost:8081"
              - url: "http://localhost:8082"
      myfolder:
        folder:
          path: "/folder"
          browse: false
          spa: true
          index: true
    routers:
      test:
        # entrypoints:
        #   - web
        # tls: {}
        path: /test
        middlewares:
          - test
          - service

# declare commands to run
services:
  # service name just for information purpose
  - name: cat my file
    # command will run inside of this path
    path: "."
    # command to run
    command: cat test.yml
    # environment variables to set
    # usable with gotemplate and sprig functions
    # get variables from name in loads
    env:
      TEST: 1
      TEST2: 2
      HOSTTYPE: '{{ env "HOSTTYPE" }}'
    # env_values override the os envs but not env values in upper
    env_values:
      - mytest/env # get all env values from mytest, give map value result in template
    # inherit environment values, default is false
    inherit_env: false
    # filter of output, default is none
    filters:
      - "internal"
    # filters_values effect dynamically
    filters_values:
      - mytest/filter # get all filter values from mytest, give slice value result in template
```
