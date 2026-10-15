# Gateway API conformance

`v1.6.3-report.yaml` is the report of the Gateway API conformance suite for
turna, run with the experimental channel CRDs and the proxies in the Gateway
namespace (`proxy.gateway_namespace`), which the `GatewayInfrastructure`
tests need.

## Running

Requirements: Docker, [kind](https://kind.sigs.k8s.io/), kubectl and Go.

```sh
./deploy/conformance/run.sh
```

The script

1. creates the kind cluster `turna` unless it exists, installs the Gateway API
   experimental CRDs and MetalLB for LoadBalancer addresses,
2. builds a turna image, loads it into the cluster and deploys the controller
   with `deploy/conformance/kustomize`,
3. builds the conformance suite of the Gateway API version and runs it in a
   container on the kind network, with the features turna reports as
   supported (`go run ./deploy/conformance/features`).

The log and the report are written to `WORK` (a temporary directory unless
set). Copy the report here after a successful run.

Environment variables:

| Variable | Default | Description |
| --- | --- | --- |
| `GATEWAY_API_VERSION` | `v1.6.3` | Gateway API release of the CRDs and the suite. |
| `CLUSTER` | `turna` | kind cluster name. |
| `WORK` | temporary | Directory for the image, suite, log and report. |
| `PROFILES` | all gateway profiles | Comma separated conformance profiles. |
| `EXTRA_FEATURES` | | Features to try before they are reported as supported. |

Arguments are passed to the suite, for example a single test:

```sh
EXTRA_FEATURES=HTTPRouteCORS PROFILES=GATEWAY-HTTP ./deploy/conformance/run.sh -run-test=HTTPRouteCORS
```

Remove the cluster with `kind delete cluster --name turna`.
