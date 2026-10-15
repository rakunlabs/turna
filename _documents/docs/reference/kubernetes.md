# Kubernetes Gateway

Turna implements the [Kubernetes Gateway API](https://gateway-api.sigs.k8s.io/). It watches `GatewayClass`, `Gateway`, `HTTPRoute`, `GRPCRoute`, `TLSRoute`, `TCPRoute`, `UDPRoute`, `ReferenceGrant` and `BackendTLSPolicy` resources and routes traffic straight to pod endpoints from `EndpointSlice`s, using Turna's own load balancing, retries and passive health checks.

It passes all tests of the `GATEWAY-HTTP`, `GATEWAY-GRPC`, `GATEWAY-TLS`, `GATEWAY-TCP` and `GATEWAY-UDP` conformance profiles of Gateway API v1.6.3 (core and extended, experimental channel, proxies in the Gateway namespace). The report and the script to run the suite in a kind cluster are in `deploy/conformance`.

## Install

Install the Gateway API CRDs, then choose Helm or Kustomize:

```sh
kubectl apply --server-side -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.6.3/standard-install.yaml
```

### Helm

```sh
helm install turna deploy/helm/turna-gateway -n turna-system --create-namespace
```

### Kustomize

From the repository root, without Helm:

```sh
kubectl apply -k deploy/kustomize/turna-gateway
kubectl rollout status deployment/turna -n turna-system
```

The manifests include the namespace, controller Deployment, generated ConfigMap,
controller/proxy ServiceAccounts, RBAC, GatewayClass and PodDisruptionBudget.
Edit `deploy/kustomize/turna-gateway/turna.yaml` for proxy and provider settings,
and `kustomization.yaml` for the namespace and image tag. Proxies run the image
of the controller pod unless `proxy.image` is set. Config changes trigger a
controller rollout through the generated ConfigMap's content hash.

Both installations default to controller mode and create a `GatewayClass` named `turna`. They are alternatives; do not apply both with the default resource names. Create a Gateway and route traffic to a Service:

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
| `controller` | Default in Helm and Kustomize. The controller creates a Deployment, Service and ConfigMap per Gateway in its namespace and reports status. Each Gateway gets its own address and is fully isolated. |
| `proxy` | Runs inside the pods created by the controller and serves one Gateway. |
| `standalone` | One Deployment serves every Gateway through one shared Service. Listeners of different Gateways on the same port share a socket and are separated by hostname only. |

In controller mode, `spec.infrastructure.labels` and `spec.infrastructure.annotations` of a Gateway are added to its proxy Service and pods (and to its ServiceAccount with gateway placement), for example to configure a cloud load balancer. They cannot replace the labels turna uses to own and select the resources.

### Proxy placement

By default the proxies of all Gateways are created in the controller namespace and share the ServiceAccount `proxy.service_account`.

With `proxy.gateway_namespace: true` (chart: `proxy.placement=gateway`, Kustomize: `deploy/kustomize/turna-gateway-gateway-namespace`) the Deployment, Service, ConfigMap, certificate Secret and PodDisruptionBudget of a Gateway are created in the Gateway's namespace, so the Gateway owner sees and debugs the proxy next to the Gateway. Each proxy gets its own ServiceAccount, a Role that reads only its certificate Secret, and a ClusterRoleBinding to `proxy.cluster_role` for the watched resources. The controller then needs cluster-wide write access to these resource kinds and the `bind` verb on `proxy.cluster_role`; the chart and the overlay create this ClusterRole. It cannot be combined with `namespace_scoped`.

Switching the placement of an existing installation creates new proxies in the new place and removes the old ones, which can change the load balancer addresses of the Gateways.

Each proxy Deployment spreads its replicas over nodes and zones. With more than one replica it also gets a PodDisruptionBudget with `maxUnavailable: 1`, so node drains and upgrades keep the Gateway serving. A terminating proxy keeps serving for `proxy.shutdown_delay` (preStop sleep, Kubernetes 1.30 or later) while load balancers stop sending it new connections.

Proxies do not read Secrets of the watched namespaces. The controller copies the certificates referenced by a Gateway's listeners, after the ReferenceGrant checks, into a Secret next to the proxy; the proxy watches only that Secret.

Only the elected leader provisions proxies and writes status. Standby controllers still watch resources and can take over when leadership changes. Disabling status updates does not disable controller-mode leader election; `status.leader_election.disabled` is intended only for single-replica installations.

Proxy resources carry a controller ownership label. The chart sets `controller_id` to the release namespace and name; the Kustomize configuration uses the stable ID `turna-kustomize`. All replicas of an installation must share this ID, and independent installations must use different IDs. For other configurations, set an explicit stable `controller_id` if you plan to change the watched namespaces or Gateways. Otherwise, the ID is derived from the controller name, watch scope and election lease configuration.

## Namespace scoped installation

A namespace owner can run their own turna for Gateways in their namespace without cluster-wide permissions. A cluster administrator does the cluster-level part once per tenant: installs the Gateway API CRDs, creates a GatewayClass with a tenant-specific `controllerName`, and allows the tenant's ServiceAccounts to read that GatewayClass. `deploy/examples/namespace-scoped-admin.yaml` contains these resources for the namespace `team-a`.

The tenant then installs the chart in their namespace:

```sh
helm install turna deploy/helm/turna-gateway -n team-a \
  --set scope=namespace \
  --set gatewayClass.create=false \
  --set gatewayClass.name=turna-team-a \
  --set controllerName=rakunlabs.com/turna-team-a
```

With `scope=namespace` the chart creates only Roles and RoleBindings and sets `namespace_scoped`, `namespaces` (the release namespace) and `gateway_class` in the configuration. Without Helm, apply the Kustomize overlay `deploy/kustomize/turna-gateway-namespaced` after setting the tenant namespace in its `namespace-transformer.yaml` and the class in its `turna.yaml`. When `namespaces` is empty, `namespace_scoped` watches the pod namespace. The installation then:

- watches only the GatewayClass named `gateway_class`, so RBAC can limit access with `resourceNames`;
- watches Gateways, routes, Services, EndpointSlices, Secrets and ReferenceGrants only in its namespace. Routes and backends in other namespaces are not visible;
- does not watch Namespace objects, so `allowedRoutes.namespaces.from: Selector` matches no routes; use `Same`;
- does not write GatewayClass status, which needs cluster-wide write access. Gateway and route status are written as usual.

Use a separate GatewayClass and `controllerName` for every tenant. `gateway_class` can also be set in cluster scope to serve only one class of a shared `controllerName`.

## Configuration

The chart writes this configuration; it can be extended with `extraConfig`.

```yaml
providers:
  kubernetes:
    enabled: true
    mode: controller
    controller_name: rakunlabs.com/turna
    controller_id: turna-system/turna # stable installation identity
    namespaces: []          # empty watches all namespaces
    gateway_class: ""       # serve and watch only this GatewayClass
    namespace_scoped: false # no cluster-wide access, needs gateway_class; empty namespaces watches the pod namespace
    gateways: []            # "namespace/name", empty serves all of the class
    certificate_secret: ""  # proxy: "namespace/name" certificate bundle, set by the controller
    port_offset: 8000       # listener 80 binds 8080 inside the pod
    address: ""             # bind host
    health_address: ":9901" # /healthz and /ready
    debounce: 200ms
    backend:
      retry: 2              # retries on another endpoint when unreachable
      strategy: round_robin # round_robin, random or least_conn
      use_service_ip: false # proxy to the cluster IP instead of endpoints
      allow_external_name: false # permit ExternalName Services as backends
    status:
      disabled: false
      addresses: []         # standalone: addresses reported on Gateways
      service: ""           # standalone: "namespace/name" of the exposing Service
      leader_election:
        disabled: false
        namespace: ""
        name: turna-gateway
    proxy:                  # controller mode
      namespace: ""         # needs the controller and proxy Roles in that namespace
      gateway_namespace: false # create proxies in the namespace of each Gateway
      cluster_role: ""      # gateway_namespace: ClusterRole bound to each proxy ServiceAccount
      image: ""             # empty uses the image of the controller pod (POD_NAME)
      service_account: turna-proxy # not used with gateway_namespace
      addresses_annotation: "" # Service annotation for Gateway spec.addresses, e.g. metallb.io/loadBalancerIPs
      replicas: 1           # more than 1 adds a PodDisruptionBudget
      service_type: LoadBalancer
      health_port: 9901     # listeners on port + port_offset == health_port are rejected
      shutdown_delay: 5s    # preStop sleep, negative disables
```

A listener whose local port (`port + port_offset`) equals the health port is not accepted (`PortUnavailable`); with the default offset of 8000 this is listener port 1901.

The Kubernetes configuration is a separate layer next to the static `server` configuration. Static entrypoints, routers and middlewares keep working, but their names must not collide with generated ones (prefixed `k8s-`).

## Supported features

- Listeners:
  - `HTTP`, and `HTTPS` with `Terminate` TLS from `kubernetes.io/tls` Secrets, also across namespaces with a `ReferenceGrant`.
  - Client certificate validation of HTTPS listeners (`spec.tls.frontend`): `default` and `perPort` validation with CA bundles in `ca.crt` of ConfigMaps, also across namespaces with a `ReferenceGrant`. `AllowValidOnly` rejects the TLS handshake without a certificate signed by one of the CAs; `AllowInsecureFallback` accepts every client and sets the Gateway condition `InsecureFrontendValidationMode`. A listener with an invalid CA reference is not served (`ResolvedRefs=False` with `InvalidCACertificateRef`, `InvalidCACertificateKind` or `RefNotPermitted`, `Accepted=False` with `NoValidCACertificate`). In standalone mode, HTTPS listeners of different Gateways on one port must use the same validation. Proxies receive the CA certificates in their bundle Secret.
  - `TLS` with `Passthrough` or `Terminate`; both modes can share a port and are selected by SNI.
  - `TCP` and `UDP`; a TCP and a UDP listener can use the same port number.
- Hostnames: exact and wildcard on listeners and routes, with the intersection rules of the specification. Listeners sharing a port are isolated: a request is handled only by the routes of the most specific listener matching its host. On HTTPS listeners sharing a port, a request whose `Host` header selects another listener than its TLS server name (for example on a reused HTTP/2 connection) is answered `421 Misdirected Request`.
- `HTTPRoute`:
  - Matches: path (`Exact`, `PathPrefix`, `RegularExpression`), method, headers, query parameters.
  - Filters: request and response header modifiers, redirects (scheme, host, port, path, 301/302/303/307/308), URL rewrites, request mirrors (also several and by percentage), `CORS` (experimental channel CRDs).
  - Request and `backendRequest` timeouts. Without a route `retry`, unreachable endpoints are retried right away on another endpoint, so the `backendRequest` timeout also bounds the whole request; the shorter of both applies.
  - `retry` (experimental channel CRDs): `codes`, `attempts` and `backoff`. Without `codes`, 502, 503 and 504 are retried. Connection errors and timed out attempts are always retried, also when their status is not in `codes`; a backend answering one of these statuses itself is retried only when the status is in `codes`. `attempts: 0` disables retries. Each attempt is bounded by `backendRequest` and all of them by `request`; a timed out attempt answers 504. Backends of a rule with `retry` do not retry unreachable endpoints themselves (`backend.retry` is not applied), so the number of attempts is exactly `attempts + 1`. Request bodies up to 1MiB are buffered for retries, larger requests are sent once.
- `GRPCRoute`: service and method matches (exact and regex), header matches, header modifier and mirror filters. Plain text gRPC uses HTTP/2 without TLS (h2c) on HTTP listeners; gateway errors are returned as gRPC status codes. HTTPRoutes and GRPCRoutes can share a listener; on a hostname conflict the older route wins.
- `TLSRoute`: routed by SNI to weighted backends; terminated connections reach the backend as plain TCP.
- `TCPRoute` and `UDPRoute`: weighted backends. A listener without hostnames cannot tell routes apart, so the oldest attached route receives the traffic.
- Backends: weighted `Service` references, cross-namespace with a `ReferenceGrant`, per-backend filters. Service ports with `appProtocol: kubernetes.io/h2c` are proxied as HTTP/2 without TLS; WebSocket upgrades are proxied on every backend (`kubernetes.io/ws`, and `kubernetes.io/wss` over TLS). `ExternalName` Services are rejected with `ResolvedRefs=False` unless `backend.allow_external_name` (chart: `allowExternalNameBackends`) is enabled, because they let route authors send traffic to any host, such as cloud metadata endpoints. Invalid backends answer their share of requests with an error (HTTP 500, gRPC `Internal`) or close their share of connections.
- `BackendTLSPolicy`: HTTPRoute and GRPCRoute backends and mirrors connect to the targeted Service, or the port named by `sectionName`, over TLS. The certificate is verified against the CA bundles in `ca.crt` of the referenced ConfigMaps (or the system roots with `wellKnownCACertificates: System`), `hostname` is sent as SNI and checked unless `subjectAltNames` (hostname and URI) are set. Policies naming a section win over policies for the whole Service, then the older policy; the losing policy gets `Accepted=False` with reason `Conflicted` for the Gateways using the conflicted target, while its other targets still apply. A policy without a usable CA certificate gets `Accepted=False` (`NoValidCACertificate`) and its backends fail instead of connecting without verification. TLSRoute and TCPRoute backends are not affected. With `spec.tls.backend.clientCertificateRef` (a `kubernetes.io/tls` Secret, across namespaces with a `ReferenceGrant`) the Gateway presents that certificate to these TLS backends. An invalid reference keeps the Gateway serving without a client certificate and sets the Gateway condition `ResolvedRefs=False` (`InvalidClientCertificateRef` or `RefNotPermitted`). A route attached to Gateways with different client certificates presents none. Proxies receive only the CA certificates of the policies used by their Gateway in their bundle Secret and need no ConfigMap access. The controller caches only the `ca.crt` key of ConfigMaps. Policy status lists at most 16 Gateways, conflicted ones first.
- `allowedRoutes` with `Same`, `All` and `Selector` namespaces and route kinds; `parentRef` `sectionName` and `port`.
- `ListenerSet`: listeners added to a Gateway that allows them with `spec.allowedListeners` (`Same`, `All` or `Selector` namespaces; none by default). They are served by the proxy of the Gateway. Routes attach with a `ListenerSet` parentRef; a Gateway parentRef reaches only the listeners of the Gateway itself. `allowedRoutes: Same` and certificate references are relative to the ListenerSet namespace, and certificates of another namespace need a `ReferenceGrant` from `ListenerSet`. On port, protocol or hostname conflicts the Gateway listeners win, then older ListenerSets (then by namespace/name). A ListenerSet without a valid listener, or whose Gateway is not accepted, is not accepted and serves nothing; the Gateway reports the accepted ones in `attachedListenerSets`. Gateway listeners also win conflicts with ListenerSets of other Gateways sharing a port in standalone mode.
- Gateway `spec.addresses` (controller mode with LoadBalancer proxy Services): IP addresses are requested from the load balancer with `spec.loadBalancerIP`, or with the annotation named by `proxy.addresses_annotation` (for example `metallb.io/loadBalancerIPs`), which also allows several addresses. An address without a value asks for any address. A requested address the load balancer does not assign sets `Programmed=False` with `AddressNotUsable`. A `spec.loadBalancerIP` set by others on the proxy Service is kept.
- Status for GatewayClass, Gateway, listeners, ListenerSets, route parents of all route kinds and BackendTLSPolicy ancestors.

Route kinds, `ListenerSet` and `BackendTLSPolicy` are skipped when their CRD is not installed. Installed route kinds are checked every minute, so CRDs installed later are picked up without a restart.

`ReferenceGrant` is watched as `v1`. Clusters with older CRDs that serve only `v1beta1` keep working; the watch switches to `v1` within a minute after the CRDs are upgraded.

Not yet supported: external auth filters, `sessionPersistence` and `spec.infrastructure.parametersRef` on Gateways.

A rule with `sessionPersistence` keeps routing without it, and the route gets `ResolvedRefs=False` with reason `UnsupportedValue` so the missing behavior is visible.

Unsupported fields that would change security or placement are rejected instead of ignored:

- A Gateway with `spec.infrastructure.parametersRef`, or with an address in `spec.addresses` that is not an IP address, is not accepted and gets no proxy.
- An HTTPS listener whose client certificate validation (`spec.tls.frontend`) has an invalid CA reference is not accepted, so it never serves without the required mutual TLS. Other listeners of the Gateway keep serving.

## Runtime updates

Changes are applied without restarting or dropping connections:

- routes, filters and endpoints are swapped atomically;
- new listener ports are opened and removed ones closed;
- TLS certificates apply to new connections;
- unchanged backends keep their balancing and health state;
- an invalid configuration is rejected and the previous one keeps serving.

Transient apply and provisioning failures are retried with exponential backoff (200ms up to 30s), even without another resource event. Proxy resource, label and image pull configuration changes are reconciled into the Deployment. Mirror filters sample their percentage once per Service and then choose one endpoint, rather than sampling each endpoint independently. The mirror middleware's body-size and concurrency limits still apply.

### Upgrading

ListenerSet support adds read access to `listenersets` and write access to `listenersets/status`. With the old RBAC, ListenerSets are not watched and a warning is logged.

BackendTLSPolicy support adds read access to `backendtlspolicies`, write access to `backendtlspolicies/status`, and cluster-wide read access to ConfigMaps for the controller. Apply the new RBAC together with the new image. With the old RBAC, BackendTLSPolicies are not watched and a warning is logged; they are picked up within a minute after the RBAC is updated. When the access review itself fails (for example the API server is unavailable), discovery is retried instead of assuming the permission.

The controller now needs `get` on its own pod, write access to Secrets and PodDisruptionBudgets in its namespace, and cluster-wide read access to Secrets; proxies lose cluster-wide Secret access and read only Secrets in the controller namespace. Apply the new RBAC together with the new image. `ExternalName` backends stop working until `backend.allow_external_name` is enabled.

### Upgrading from the initial Gateway implementation

Generated proxy names now always include a hash of the complete Gateway namespace/name to avoid ambiguous names such as `a-b/c` and `a/b-c`. Existing proxies using the old unhashed names are not adopted or deleted automatically because they have no installation ownership label. Wait for the new proxy Deployment and Service to become ready, switch traffic to the new Service address, and then remove the old Deployment, Service and ConfigMap after verifying which installation owns them. This can allocate a new load balancer address.

The same mechanism reloads `server.load_value` when it comes from a dynamic Consul source.
