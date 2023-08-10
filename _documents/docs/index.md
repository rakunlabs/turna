---
# https://vitepress.dev/reference/default-theme-home-page
layout: home

hero:
  name: "turna"
  text: "documentation"
  # tagline: documentation
  image:
    light: /assets/turna.svg
    dark: /assets/turna_light.svg
    alt: turna
  actions:
    - theme: brand
      text: Getting Started
      link: /introduction/getting-started
    - theme: alt
      text: Reference
      link: /reference/config

features:
  - title: Config Loader
    details: Load configuration from multiple sources
  - title: Reverse Proxy
    details: Serve applications with various middlewares
  - title: Runner
    details: Run multiple applications
---
