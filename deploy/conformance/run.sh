#!/usr/bin/env bash
# Runs the Gateway API conformance suite against turna in a kind cluster.
# See deploy/conformance/README.md.
set -euo pipefail

GATEWAY_API_VERSION="${GATEWAY_API_VERSION:-v1.6.3}"
CLUSTER="${CLUSTER:-turna}"
CONTEXT="kind-${CLUSTER}"
WORK="${WORK:-$(mktemp -d)}"
REPO="$(cd "$(dirname "$0")/../.." && pwd)"
ARCH="$(docker info --format '{{.Architecture}}' | sed -e 's/x86_64/amd64/' -e 's/aarch64/arm64/')"

# GATEWAY-HTTP and GATEWAY-GRPC run with the experimental channel CRDs, so
# HTTPRoute retry fields are present.
PROFILES="${PROFILES:-GATEWAY-HTTP,GATEWAY-GRPC,GATEWAY-TLS,GATEWAY-TCP,GATEWAY-UDP}"
# extra conformance flags, for example -run-test=HTTPRouteRetry
EXTRA_ARGS=("$@")

log() { printf '==> %s\n' "$*" >&2; }

cluster() {
	if ! kind get clusters | grep -qx "$CLUSTER"; then
		log "creating kind cluster $CLUSTER"
		kind create cluster --name "$CLUSTER"
	fi

	log "installing Gateway API $GATEWAY_API_VERSION experimental CRDs"
	kubectl --context "$CONTEXT" apply --server-side -f \
		"https://github.com/kubernetes-sigs/gateway-api/releases/download/${GATEWAY_API_VERSION}/experimental-install.yaml" >/dev/null

	log "installing MetalLB for LoadBalancer addresses"
	kubectl --context "$CONTEXT" apply -f https://raw.githubusercontent.com/metallb/metallb/v0.15.2/config/manifests/metallb-native.yaml >/dev/null
	kubectl --context "$CONTEXT" -n metallb-system wait --for=condition=Available deploy/controller --timeout=300s >/dev/null

	subnet="$(docker network inspect kind -f '{{range .IPAM.Config}}{{.Subnet}} {{end}}' | tr ' ' '\n' | grep -m1 '\.')"
	prefix="$(echo "$subnet" | cut -d. -f1-2)"
	kubectl --context "$CONTEXT" apply -f - >/dev/null <<EOF
apiVersion: metallb.io/v1beta1
kind: IPAddressPool
metadata: {name: kind, namespace: metallb-system}
spec: {addresses: ["${prefix}.255.1-${prefix}.255.250"]}
---
apiVersion: metallb.io/v1beta1
kind: L2Advertisement
metadata: {name: kind, namespace: metallb-system}
EOF
}

turna() {
	log "building turna:conformance for linux/$ARCH"
	mkdir -p "$WORK/image"
	(cd "$REPO" && CGO_ENABLED=0 GOOS=linux GOARCH="$ARCH" go build -trimpath -ldflags="-s -w" -o "$WORK/image/turna" ./cmd/turna)
	docker build -q -t turna:conformance -f "$REPO/ci/alpine.Dockerfile" "$WORK/image" >/dev/null
	kind load docker-image turna:conformance --name "$CLUSTER" >/dev/null

	log "deploying turna"
	kubectl --context "$CONTEXT" apply -k "$REPO/deploy/conformance/kustomize" >/dev/null
	# a new image with the same tag needs a restart
	kubectl --context "$CONTEXT" -n turna-system rollout restart deploy/turna >/dev/null
	kubectl --context "$CONTEXT" -n turna-system rollout status deploy/turna --timeout=180s >/dev/null
	# proxies of a previous run use the old image
	kubectl --context "$CONTEXT" -n turna-system delete pods -l app.kubernetes.io/name=turna-gateway-proxy --ignore-not-found >/dev/null
}

suite() {
	log "building the conformance suite $GATEWAY_API_VERSION"
	if [ ! -d "$WORK/gateway-api" ]; then
		git clone -q --depth 1 --branch "$GATEWAY_API_VERSION" https://github.com/kubernetes-sigs/gateway-api.git "$WORK/gateway-api" 2>/dev/null
	fi
	(cd "$WORK/gateway-api/conformance" && CGO_ENABLED=0 GOOS=linux GOARCH="$ARCH" go test -c -o "$WORK/conformance.test" .)
	kind get kubeconfig --internal --name "$CLUSTER" >"$WORK/kubeconfig"

	features="$(cd "$REPO" && go run ./deploy/conformance/features)"
	# EXTRA_FEATURES tries features before they are reported as supported
	if [ -n "${EXTRA_FEATURES:-}" ]; then
		features="$features,$EXTRA_FEATURES"
	fi

	# GatewayStaticAddresses: the last address of the MetalLB pool is usable,
	# an address outside of it is not
	subnet="$(docker network inspect kind -f '{{range .IPAM.Config}}{{.Subnet}} {{end}}' | tr ' ' '\n' | grep -m1 '\.')"
	prefix="$(echo "$subnet" | cut -d. -f1-2)"

	# the suite runs in the kind network to reach the LoadBalancer addresses
	log "running profiles $PROFILES"
	docker rm -f turna-conformance >/dev/null 2>&1 || true
	docker create --name turna-conformance --network kind -e KUBECONFIG=/work/kubeconfig -w /work alpine:3.24.1 \
		/work/conformance.test -test.v -test.timeout 90m -test.run '^TestConformance$' \
		-gateway-class=turna \
		-supported-features="$features" \
		-conformance-profiles="$PROFILES" \
		-organization=rakunlabs -project=turna -url=https://github.com/rakunlabs/turna \
		-version="$(cd "$REPO" && git describe --tags --always --dirty)" -contact=@rakunlabs \
		-report-output=/work/report.yaml \
		-usable-address="${prefix}.255.250" -unusable-address="${prefix}.254.1" \
		"${EXTRA_ARGS[@]}" >/dev/null
	docker cp "$WORK/conformance.test" turna-conformance:/work/conformance.test
	docker cp "$WORK/kubeconfig" turna-conformance:/work/kubeconfig

	status=0
	docker start -a turna-conformance >"$WORK/conformance.log" 2>&1 || status=$?
	docker cp turna-conformance:/work/report.yaml "$WORK/report.yaml" 2>/dev/null || true
	docker rm turna-conformance >/dev/null

	grep -E -- '^\s*--- (PASS|FAIL|SKIP)' "$WORK/conformance.log" | grep -v PASS || true
	log "log: $WORK/conformance.log"
	[ -f "$WORK/report.yaml" ] && log "report: $WORK/report.yaml"

	return "$status"
}

cluster
turna
suite
