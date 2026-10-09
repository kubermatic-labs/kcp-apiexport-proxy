#!/usr/bin/env bash

# Copyright The kcp-apiexport-proxy Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# This script creates a kind cluster and deploys kcp, Kyverno and the proxy
# (using its Helm chart, with a token) into it. See test/README.md. It then publishes an APIExport from one kcp workspace, binds it in
# another, and checks that
#   - the consumer workspace's objects can be read through the proxy, and
#   - a Kyverno policy can use the proxy to admit or deny objects based on
#     their "kcp.io/cluster" annotation.
#
# Requires Docker and curl. kind, kubectl, helm and Kyverno are used at the
# versions pinned in hack/lib.sh.
#
# Environment variables:
#   KIND_CLUSTER_NAME  name of the kind cluster (default kcp-apiexport-proxy)
#   KEEP_CLUSTER       set to true to keep the cluster after the run; its
#                      kubeconfig is in _output/kind-$KIND_CLUSTER_NAME.kubeconfig
#   PROXY_LOCAL_PORT   local port to forward to the proxy (default 18080)

set -euo pipefail

cd "$(dirname "$0")/.."
source hack/lib.sh

KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-kcp-apiexport-proxy}"
KEEP_CLUSTER="${KEEP_CLUSTER:-false}"
PROXY_LOCAL_PORT="${PROXY_LOCAL_PORT:-18080}"

MANIFESTS=test/manifests
IMAGE=kcp-apiexport-proxy:test
IMAGE_DIR="$ROOT_DIR/_output/image"
export KUBECONFIG="$ROOT_DIR/_output/kind-$KIND_CLUSTER_NAME.kubeconfig"

WORK_DIR="$(mktemp -d)"
CLUSTER_CREATED=false
PORT_FORWARD_PID=""

cleanup() {
  local rc=$?

  if [ -n "$PORT_FORWARD_PID" ]; then
    kill "$PORT_FORWARD_PID" 2> /dev/null || true
  fi

  if [ "$rc" -ne 0 ] && [ "$CLUSTER_CREATED" = "true" ]; then
    echodate "Test failed, printing logs..."
    kubectl -n kcp logs deployment/kcp --tail=50 || true
    kubectl -n kcp-apiexport-proxy logs deployment/kcp-apiexport-proxy --tail=50 || true
    kubectl -n kyverno logs -l app.kubernetes.io/component=admission-controller --tail=50 || true
  fi

  if [ "$CLUSTER_CREATED" = "true" ]; then
    if [ "$KEEP_CLUSTER" = "true" ]; then
      echodate "Keeping kind cluster $KIND_CLUSTER_NAME, use KUBECONFIG=$KUBECONFIG to access it."
    else
      echodate "Deleting kind cluster $KIND_CLUSTER_NAME..."
      kind delete cluster --name "$KIND_CLUSTER_NAME" || true
      rm -f "$KUBECONFIG"
    fi
  fi

  rm -rf "$WORK_DIR"
  exit "$rc"
}
trap cleanup EXIT

if ! docker info > /dev/null 2>&1; then
  echodate "Docker is not available, but is required to run kind."
  exit 1
fi

if ! command -v curl > /dev/null 2>&1; then
  echodate "curl is not available, but is required to talk to the proxy."
  exit 1
fi

ensure_kind
ensure_kubectl
ensure_helm
export PATH="$TOOLS_DIR:$PATH"

echodate "Building the proxy image..."
GOOS=linux GOARCH="$ARCH" make --no-print-directory build BUILD_DEST="$IMAGE_DIR"
docker build --tag "$IMAGE" --file build/Dockerfile "$IMAGE_DIR"

if kind get clusters 2> /dev/null | grep -qx "$KIND_CLUSTER_NAME"; then
  echodate "A kind cluster named $KIND_CLUSTER_NAME already exists, delete it or set KIND_CLUSTER_NAME."
  exit 1
fi

echodate "Creating kind cluster $KIND_CLUSTER_NAME..."
CLUSTER_CREATED=true
kind create cluster --name "$KIND_CLUSTER_NAME" --image "$KIND_NODE_IMAGE" --kubeconfig "$KUBECONFIG" --wait 120s
kind load docker-image "$IMAGE" --name "$KIND_CLUSTER_NAME"

echodate "Installing Kyverno $KYVERNO_VERSION..."
kubectl apply --server-side \
  -f "https://github.com/kyverno/kyverno/releases/download/v${KYVERNO_VERSION}/install.yaml" > /dev/null

echodate "Deploying kcp..."
kubectl apply -f "$MANIFESTS/cluster/kcp.yaml"
kubectl -n kcp rollout status deployment/kcp --timeout=300s

# kubectl_kcp runs kubectl inside the kcp pod as the kcp admin; stdin is
# passed through. kubectl_ws <workspace> targets a child workspace of root.
kubectl_kcp() {
  kubectl -n kcp exec -i deployment/kcp -- kubectl "$@"
}

KCP_ROOT_URL="$(kubectl_kcp config view -o jsonpath='{.clusters[?(@.name=="root")].cluster.server}')"

kubectl_ws() {
  local ws="$1"
  shift
  kubectl_kcp --server="$KCP_ROOT_URL:$ws" "$@"
}

echodate "Creating workspaces in kcp..."
kubectl_kcp apply -f - < "$MANIFESTS/kcp/workspaces.yaml"
kubectl_kcp wait --for=jsonpath='{.status.phase}'=Ready \
  workspace/provider workspace/consumer --timeout=120s

echodate "Creating the APIExport in the provider workspace..."
kubectl_ws provider apply -f - < "$MANIFESTS/kcp/provider.yaml"

echodate "Binding the APIExport in the consumer workspace..."
kubectl_ws consumer apply -f - < "$MANIFESTS/kcp/consumer.yaml"
kubectl_ws consumer wait --for=condition=Ready apibinding/example.com --timeout=120s
kubectl_ws consumer apply -f - < "$MANIFESTS/kcp/widget.yaml"

CONSUMER_CLUSTER="$(kubectl_kcp get workspace consumer -o jsonpath='{.spec.cluster}')"
echodate "Consumer workspace is logical cluster $CONSUMER_CLUSTER."

echodate "Deploying the proxy..."
# The proxy uses the kcp admin identity against the provider workspace,
# where the APIExportEndpointSlice lives. kcp's self-signed serving
# certificate does not cover the Service's hostname, hence the insecure
# TLS setting.
PROXY_KUBECONFIG="$WORK_DIR/proxy.kubeconfig"
kubectl_kcp config view --raw --minify --context=root > "$PROXY_KUBECONFIG"
kubectl --kubeconfig="$PROXY_KUBECONFIG" config unset clusters.root.certificate-authority-data > /dev/null
kubectl --kubeconfig="$PROXY_KUBECONFIG" config set-cluster root \
  --server=https://kcp.kcp.svc.cluster.local:6443/clusters/root:provider \
  --insecure-skip-tls-verify=true > /dev/null

kubectl create namespace kcp-apiexport-proxy
kubectl -n kcp-apiexport-proxy create secret generic kcp-apiexport-proxy-kubeconfig \
  --from-file=kubeconfig="$PROXY_KUBECONFIG"

helm upgrade --install kcp-apiexport-proxy deploy/charts/kcp-apiexport-proxy \
  --namespace kcp-apiexport-proxy \
  --values "$MANIFESTS/cluster/proxy-values.yaml"
kubectl -n kcp-apiexport-proxy rollout status deployment/kcp-apiexport-proxy --timeout=180s

PROXY_TOKEN="$(kubectl -n kcp-apiexport-proxy get secret kcp-apiexport-proxy-token -o jsonpath='{.data.token}' | base64 -d)"

kubectl -n kcp-apiexport-proxy port-forward service/kcp-apiexport-proxy "$PROXY_LOCAL_PORT:8080" > /dev/null &
PORT_FORWARD_PID=$!

# proxy_curl <token> <path> sends a GET request to the proxy through the
# port-forward and prints the HTTP status code followed by the body. <path>
# is relative to the example.com APIExportEndpointSlice; an empty <token>
# sends none.
proxy_curl() {
  local token="$1"
  local path="$2"
  local args=(
    --silent --show-error
    --write-out '%{http_code}\n'
    --output "$WORK_DIR/response"
  )
  if [ -n "$token" ]; then
    args+=(--header "Authorization: Bearer $token")
  fi

  curl "${args[@]}" "http://127.0.0.1:$PROXY_LOCAL_PORT/apiexportendpointslices/example.com$path"
  cat "$WORK_DIR/response"
}

# proxy_get <path> succeeds if GET <path> with the token returns 200, and
# prints the body.
proxy_get() {
  local output
  output="$(proxy_curl "$PROXY_TOKEN" "$1")" || return 1
  [ "$(head -n 1 <<< "$output")" = "200" ] || return 1
  tail -n +2 <<< "$output"
}

# expect_status <token> <path> <status> checks that GET <path> with the
# given token returns the given HTTP status code.
expect_status() {
  local output status
  output="$(proxy_curl "$1" "$2")"
  status="$(head -n 1 <<< "$output")"
  if [ "$status" != "$3" ]; then
    echodate "GET $2 returned $status, expected $3."
    return 1
  fi
  echodate "GET $2 returned $3."
}

# expect_through_proxy <path> <string> checks that GET <path> through the
# proxy eventually succeeds and returns a body containing <string>.
expect_through_proxy() {
  local path="$1"
  local want="$2"

  check() {
    proxy_get "$path" 2> /dev/null | grep -q "$want"
  }

  if ! retry 30 check; then
    echodate "GET $path through the proxy did not return a body containing: $want"
    return 1
  fi
  echodate "GET $path contains: $want"
}

echodate "Checking requests through the proxy..."
expect_through_proxy "/clusters/$CONSUMER_CLUSTER/apis/example.com/v1/widgets" '"name": *"my-widget"'
expect_through_proxy "/clusters/$CONSUMER_CLUSTER/apis/example.com/v1/namespaces/default/widgets/my-widget" '"color": *"blue"'
expect_through_proxy "/clusters/$CONSUMER_CLUSTER/apis/apis.kcp.io/v1alpha1/apibindings" '"name": *"example.com"'

expect_status "$PROXY_TOKEN" "/clusters/does-not-exist/apis/example.com/v1/widgets" 404

echodate "Checking that requests without a valid token are rejected..."
expect_status "" "/clusters/$CONSUMER_CLUSTER/apis/example.com/v1/widgets" 401
expect_status "wrong-token" "/clusters/$CONSUMER_CLUSTER/apis/example.com/v1/widgets" 401

echodate "Waiting for Kyverno..."
kubectl -n kyverno wait --for=condition=Available deployment --all --timeout=300s
kubectl apply -f "$MANIFESTS/cluster/kyverno-rbac.yaml"
kubectl apply -f "$MANIFESTS/cluster/policy.yaml"

# create_configmap <name> <cluster> creates a ConfigMap in the default
# namespace annotated with the given logical cluster.
create_configmap() {
  kubectl create configmap "$1" --from-literal=foo=bar --dry-run=client -o yaml |
    kubectl annotate --local -f - "kcp.io/cluster=$2" -o yaml |
    kubectl create -f - 2>&1
}

# The policy and the proxy's Service take a moment to become active, so
# retry until the ConfigMap for an unknown logical cluster is denied by the
# policy's validation, not by an error reaching the proxy.
expect_denied() {
  local output
  if output="$(create_configmap unknown-cluster does-not-exist)"; then
    kubectl delete configmap unknown-cluster > /dev/null
    return 1
  fi
  echo "$output" | grep -q 'has no APIBindings for this APIExport'
}

echodate "Checking that Kyverno denies a ConfigMap for an unknown logical cluster..."
retry 60 expect_denied
echodate "ConfigMap for an unknown logical cluster was denied."

echodate "Checking that Kyverno admits a ConfigMap for the consumer's logical cluster..."
retry 10 create_configmap bound-cluster "$CONSUMER_CLUSTER"
echodate "ConfigMap for the consumer's logical cluster was admitted."

echodate "Integration test passed :-)"
