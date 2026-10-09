# Kubernetes Gateway

Turna implements the [Kubernetes Gateway API](https://gateway-api.sigs.k8s.io/). It watches `GatewayClass`, `Gateway`, `HTTPRoute`, `GRPCRoute`, `TLSRoute`, `TCPRoute`, `UDPRoute` and `ReferenceGrant` resources and routes traffic straight to pod endpoints from `EndpointSlice`s, using Turna's own load balancing, retries and passive health checks.

It passes all tests of the `GATEWAY-HTTP`, `GATEWAY-GRPC`, `GATEWAY-TLS`, `GATEWAY-TCP` and `GATEWAY-UDP` conformance profiles of Gateway API v1.6.3 (core and extended). The report is in `deploy/conformance`.

## Install

Install the Gateway API CRDs and the Helm chart:

```sh
kubectl apply --server-side -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.6.3/standard-install.yaml

helm install turna deploy/helm/turna-gateway -n turna-system --create-namespace
```

The chart creates a `GatewayClass` named `turna`. Create a Gateway and route traffic to a Service:

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: web
  namespace: demo
spec:
  gatewayClassName: turna
  listeners:
    - name: http
      port: 80
      protocol: HTTP
    - name: https
      port: 443
      protocol: HTTPS
      hostname: "*.example.com"
      tls:
        certificateRefs:
          - name: example-tls
---
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: app
  namespace: demo
spec:
  parentRefs:
    - name: web
  hostnames: ["app.example.com"]
  rules:
    - backendRefs:
        - name: app
          port: 80
```

Complete examples are in `deploy/examples`: `gateway-demo.yaml` (HTTP canary routing, weights, redirects) and `gateway-streams.yaml` (gRPC, TLS passthrough, TCP and UDP).

## Modes

| Mode | Description |
| --- | --- |
| `controller` | Default in the chart. The controller creates a Deployment, Service and ConfigMap per Gateway in its namespace and reports status. Each Gateway gets its own address and is fully isolated. |
| `proxy` | Runs inside the pods created by the controller and serves one Gateway. |
| `standalone` | One Deployment serves every Gateway through one shared Service. Listeners of different Gateways on the same port share a socket and are separated by hostname only. |

In controller mode, `spec.infrastructure.labels` of a Gateway are added to its proxy pods and `spec.infrastructure.annotations` to its Service, for example to configure a cloud load balancer.

## Configuration

The chart writes this configuration; it can be extended with `extraConfig`.

```yaml
providers:
  kubernetes:
    enabled: true
    mode: controller
    controller_name: rakunlabs.com/turna
    namespaces: []          # empty watches all namespaces
    gateways: []            # "namespace/name", empty serves all of the class
    port_offset: 8000       # listener 80 binds 8080 inside the pod
    address: ""             # bind host
    health_address: ":9901" # /healthz and /ready
    debounce: 200ms
    backend:
      retry: 2              # retries on another endpoint when unreachable
      strategy: round_robin # round_robin, random or least_conn
      use_service_ip: false # proxy to the cluster IP instead of endpoints
    status:
      disabled: false
      addresses: []         # standalone: addresses reported on Gateways
      service: ""           # standalone: "namespace/name" of the exposing Service
      leader_election:
        disabled: false
        namespace: ""
        name: turna-gateway
    proxy:                  # controller mode
      namespace: ""
      image: ghcr.io/rakunlabs/turna:latest
      service_account: turna-proxy
      replicas: 1
      service_type: LoadBalancer
```

The Kubernetes configuration is a separate layer next to the static `server` configuration. Static entrypoints, routers and middlewares keep working, but their names must not collide with generated ones (prefixed `k8s-`).

## Supported features

- Listeners:
  - `HTTP`, and `HTTPS` with `Terminate` TLS from `kubernetes.io/tls` Secrets, also across namespaces with a `ReferenceGrant`.
  - `TLS` with `Passthrough` or `Terminate`; both modes can share a port and are selected by SNI.
  - `TCP` and `UDP`; a TCP and a UDP listener can use the same port number.
- Hostnames: exact and wildcard on listeners and routes, with the intersection rules of the specification.
- `HTTPRoute`:
  - Matches: path (`Exact`, `PathPrefix`, `RegularExpression`), method, headers, query parameters.
  - Filters: request and response header modifiers, redirects (scheme, host, port, path, 301/302/303/307/308), URL rewrites, request mirrors (also several and by percentage).
  - Request timeouts.
- `GRPCRoute`: service and method matches (exact and regex), header matches, header modifier and mirror filters. Plain text gRPC uses HTTP/2 without TLS (h2c) on HTTP listeners; gateway errors are returned as gRPC status codes. HTTPRoutes and GRPCRoutes can share a listener; on a hostname conflict the older route wins.
- `TLSRoute`: routed by SNI to weighted backends; terminated connections reach the backend as plain TCP.
- `TCPRoute` and `UDPRoute`: weighted backends. A listener without hostnames cannot tell routes apart, so the oldest attached route receives the traffic.
- Backends: weighted `Service` references, cross-namespace with a `ReferenceGrant`, `ExternalName` Services, per-backend filters. Invalid backends answer their share of requests with an error (HTTP 500, gRPC `Internal`) or close their share of connections.
- `allowedRoutes` with `Same`, `All` and `Selector` namespaces and route kinds; `parentRef` `sectionName` and `port`.
- Status for GatewayClass, Gateway, listeners and route parents of all route kinds.

Route kinds whose CRD is not installed are skipped.

Not yet supported: `BackendTLSPolicy`, `ListenerSet`, CORS and external auth filters, backend timeouts and retries from the route, and `spec.addresses` on Gateways.

## Runtime updates

Changes are applied without restarting or dropping connections:

- routes, filters and endpoints are swapped atomically;
- new listener ports are opened and removed ones closed;
- TLS certificates apply to new connections;
- unchanged backends keep their balancing and health state;
- an invalid configuration is rejected and the previous one keeps serving.

The same mechanism reloads `server.load_value` when it comes from a dynamic Consul source.
