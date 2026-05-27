![turna](_documents/docs/public/assets/turna.svg#gh-light-mode-only)
![turna](_documents/docs/public/assets/turna_light.svg#gh-dark-mode-only)

[![License](https://img.shields.io/github/license/rakunlabs/turna?color=blue&style=flat-square)](https://raw.githubusercontent.com/rakunlabs/turna/main/LICENSE)
[![Coverage](https://img.shields.io/sonar/coverage/rakunlabs_turna?logo=sonarcloud&server=https%3A%2F%2Fsonarcloud.io&style=flat-square)](https://sonarcloud.io/summary/overall?id=rakunlabs_turna)
[![GitHub Workflow Status](https://img.shields.io/github/actions/workflow/status/rakunlabs/turna/test.yml?branch=main&logo=github&style=flat-square&label=ci)](https://github.com/rakunlabs/turna/actions)
[![Go Report Card](https://goreportcard.com/badge/github.com/rakunlabs/turna?style=flat-square)](https://goreportcard.com/report/github.com/rakunlabs/turna)
[![Web](https://img.shields.io/badge/web-document-blueviolet?style=flat-square)](https://rakunlabs.github.io/turna/)

Turna is a small operations sidecar for applications. It can load configuration from files, environment variables, Consul, and Vault; prepare files before startup; serve HTTP/TCP traffic; and run local processes in dependency order.

Use Turna when an application needs runtime configuration, a reverse proxy, static file serving, OAuth/session helpers, or a simple process runner without adding that logic to the application itself.

## Installation

Download a release binary for your platform from the [releases page](https://github.com/rakunlabs/turna/releases/latest).

```sh
curl -fSL https://github.com/rakunlabs/turna/releases/latest/download/turna_Linux_x86_64.tar.gz | tar -xz --overwrite -C ~/bin/ turna
```

## Documentation

Full documentation is in [`_documents/docs`](_documents/docs) and is published at <https://rakunlabs.github.io/turna/>.
