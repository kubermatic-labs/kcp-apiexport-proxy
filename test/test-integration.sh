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
# into it. It then publishes an APIExport from one kcp workspace, binds it in
# another, and checks that
#   - the consumer workspace's objects can be read through the proxy, and
#   - a Kyverno policy can use the proxy to admit or deny objects based on
#     their "kcp.io/cluster" annotation.
#
# Requires Docker. kind, kubectl and Kyverno are used at the versions pinned
# in hack/lib.sh.
#
# Environment variables:
#   KIND_CLUSTER_NAME  name of the kind cluster (default kcp-apiexport-proxy)
#   KEEP_CLUSTER       set to true to keep the cluster after the run; its
#                      kubeconfig is in _output/kind-$KIND_CLUSTER_NAME.kubeconfig

set -euo pipefail

cd "$(dirname "$0")/.."
source hack/lib.sh

KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-kcp-apiexport-proxy}"
KEEP_CLUSTER="${KEEP_CLUSTER:-false}"

MANIFESTS=test/manifests
IMAGE=kcp-apiexport-proxy:test
IMAGE_DIR="$ROOT_DIR/_output/image"
export KUBECONFIG="$ROOT_DIR/_output/kind-$KIND_CLUSTER_NAME.kubeconfig"

WORK_DIR="$(mktemp -d)"
CLUSTER_CREATED=false

cleanup() {
  local rc=$?

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

ensure_kind
ensure_kubectl
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

kubectl apply -f "$MANIFESTS/cluster/proxy.yaml"
kubectl -n kcp-apiexport-proxy create secret generic kcp-apiexport-proxy-kubeconfig \
  --from-file=kubeconfig="$PROXY_KUBECONFIG"
kubectl -n kcp-apiexport-proxy rollout status deployment/kcp-apiexport-proxy --timeout=120s

# proxy_get <path> sends a GET request to the proxy through the kind
# cluster's API server service proxy.
proxy_get() {
  kubectl get --raw "/api/v1/namespaces/kcp-apiexport-proxy/services/http:kcp-apiexport-proxy:http/proxy$1"
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

if proxy_get "/clusters/does-not-exist/apis/example.com/v1/widgets" > /dev/null 2>&1; then
  echodate "Expected a request for an unknown logical cluster to fail."
  exit 1
fi
echodate "Request for an unknown logical cluster was rejected."

echodate "Waiting for Kyverno..."
kubectl -n kyverno wait --for=condition=Available deployment --all --timeout=300s
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
