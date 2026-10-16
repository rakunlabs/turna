---
layout: home

hero:
  text: Load config, prepare files, route traffic, and run services from one binary.
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
    details: Load and merge config from files, env, HTTP, Consul, Vault, AWS, Azure Key Vault, and GCP secrets.
    link: /reference/loads
    linkText: Loads
  - title: HTTP server
    details: Build reverse proxies, static file servers, and forward proxies from ordered middleware chains.
    link: /reference/server/server
    linkText: Server
  - title: Middleware chains
    details: Compose auth, sessions, routing, headers, CORS, compression, rewrites, logging, IAM, and OAuth2 helpers.
    link: /reference/server/http/middlewares/
    linkText: HTTP middlewares
  - title: TCP and UDP
    details: Run TCP redirects, SOCKS5 and SSH endpoints, TLS termination, DNS servers, and UDP load balancing.
    link: /reference/server/tcp/middlewares/socks5
    linkText: TCP middlewares
  - title: Kubernetes Gateway
    details: Implements the Gateway API and routes traffic straight to pod endpoints with Turna's own load balancing.
    link: /reference/kubernetes
    linkText: Kubernetes Gateway
  - title: Process runner
    details: Start commands with templated environment variables, dependency ordering, filtering, and failure policy.
    link: /reference/services
    linkText: Services
---

<div class="home-quickstart">

## Quick start

Download the binary:

```sh
curl -fSL https://github.com/rakunlabs/turna/releases/latest/download/turna_Linux_x86_64.tar.gz \
  | tar -xz --overwrite -C ~/bin/ turna
```

Create `turna.yaml` to proxy `:8080` to an app on port `3000`:

```yaml
server:
  entrypoints:
    web:
      address: ":8080"
  http:
    middlewares:
      app:
        service:
          loadbalancer:
            servers:
              - url: "http://localhost:3000"
    routers:
      app:
        path: /*
        middlewares:
          - app
```

Run `turna` in the same directory. Other platforms and more examples are in [Getting Started](/introduction/getting-started) and [Examples](/examples/).

</div>
