---
layout: home

hero:
  name: "turna"
  tagline: Load config, prepare files, route traffic, and run services from one small binary.
  image:
    light: /assets/turna.svg
    dark: /assets/turna_light.svg
    alt: turna
  actions:
    - theme: brand
      text: Get Started
      link: /introduction/getting-started
    - theme: alt
      text: Configuration Reference
      link: /reference/config

features:
  - title: Runtime configuration
    details: Load and merge data from files, environment variables, Consul, Vault, and in-memory content.
  - title: HTTP and TCP server
    details: Build reverse proxies, static file servers, forward proxies, TCP redirects, and SOCKS5 endpoints.
  - title: Middleware chains
    details: Compose auth, sessions, routing, headers, CORS, compression, rewrites, logging, IAM, and OAuth2 helpers.
  - title: Process runner
    details: Start commands with templated environment variables, dependency ordering, filtering, and failure policy.
---
