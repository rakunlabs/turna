# Turna Gateway with Kustomize

A plain Kustomize installation, without Helm. Run these commands from the
repository root:

```sh
kubectl apply --server-side -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.6.3/standard-install.yaml
kubectl apply -k deploy/kustomize/turna-gateway
kubectl rollout status deployment/turna -n turna-system
```

Preview the manifests without contacting a cluster:

```sh
kubectl kustomize deploy/kustomize/turna-gateway
```

## Defaults

- Namespace: `turna-system`.
- GatewayClass: `turna`, controller name: `rakunlabs.com/turna`.
- Controller mode with two controller replicas and leader election.
- Two proxy replicas and a LoadBalancer Service per Gateway.
- Listener ports are mapped with an offset of 8000 (80 → 8080).
- Non-root containers, read-only root filesystems and dropped capabilities.
- Separate controller and proxy ServiceAccounts; proxies have read-only RBAC
  and no access to Secrets outside this namespace.
- A PodDisruptionBudget preserves one controller replica during voluntary disruptions.
- Each Gateway proxy gets its own PodDisruptionBudget, spreads its replicas
  over nodes and zones, and drains connections for 5s before stopping.

The controller itself does not expose a traffic Service. It creates a Deployment,
ConfigMap and Service for each Gateway. Use the same Gateway and route manifests
as the Helm installation, for example `deploy/examples/gateway-demo.yaml`.

## Customize

For a simple installation, edit the files in this directory:

| Setting | File |
| --- | --- |
| Namespace | `namespace` in `kustomization.yaml` |
| Controller and proxy image tag | `images[].newTag` in `kustomization.yaml` |
| Proxy replicas, Service type, resources, watch scope | `turna.yaml` |
| Controller replicas and resources | `deployment.yaml` |

Proxies run the image of the controller pod unless
`providers.kubernetes.proxy.image` is set, so an image change in an overlay
applies to both. Pin a released tag or digest for production instead of using
`latest`.

The controller creates proxy resources in its own namespace. To use another
`proxy.namespace`, copy the `turna` and `turna-proxy` Roles and RoleBindings
into that namespace.

When editing this base directly, Kustomize rewrites RBAC subject namespaces along with the namespace setting.
The leader election lease and proxy resources use `POD_NAMESPACE`, so no embedded
namespace strings in the Turna configuration need to be changed.

`configMapGenerator` adds a content hash to the controller ConfigMap name and
rewrites its Deployment volume reference. Configuration changes therefore roll
the controller automatically. Old generated ConfigMaps are not automatically
pruned by `kubectl apply -k`.

You can also reference this directory as a base in your own overlay. If replacing
the Turna configuration, use a generator with `behavior: merge`:

```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
namespace: turna-system
resources:
  - ../../deploy/kustomize/turna-gateway # adjust to your overlay's location
configMapGenerator:
  - name: turna-config
    behavior: merge
    files:
      - turna.yaml # your complete Turna configuration
```

If an overlay changes the namespace, add an explicit namespace transformer so
RBAC subjects already namespaced by the base are updated too:

```yaml
# namespace-transformer.yaml
apiVersion: builtin
kind: NamespaceTransformer
metadata:
  name: namespace
  namespace: my-turna-system
setRoleBindingSubjects: allServiceAccounts
fieldSpecs:
  - path: metadata/namespace
    create: true
  - kind: Namespace
    path: metadata/name
```

Set `namespace: my-turna-system` and `transformers: [namespace-transformer.yaml]`
in the overlay's `kustomization.yaml`.

This is an alternative to Helm, not an additional deployment alongside the same
Helm release: the default resource names overlap. For independent installations,
use distinct resource names, GatewayClasses, controller names, installation IDs
and election leases. Name prefixes do not rewrite the proxy ServiceAccount string
inside `turna.yaml`; update it explicitly if renaming the ServiceAccounts.
