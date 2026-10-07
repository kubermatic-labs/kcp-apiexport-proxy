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

# This script builds the proxy, starts a local kcp instance, publishes an
# APIExport from one workspace, binds it in another, and then checks that
# the consumer workspace's objects can be read through the proxy.
#
# The following environment variables can be used to avoid port conflicts:
#   KCP_PORT (default 6443), ETCD_CLIENT_PORT (default 2379),
#   ETCD_PEER_PORT (default 2380), PROXY_PORT (default 8080).
# Set KEEP_WORK_DIR=true to keep kcp's data and the logs after the run.

set -euo pipefail

cd "$(dirname "$0")/.."
source hack/lib.sh

KCP_PORT="${KCP_PORT:-6443}"
ETCD_CLIENT_PORT="${ETCD_CLIENT_PORT:-2379}"
ETCD_PEER_PORT="${ETCD_PEER_PORT:-2380}"
PROXY_PORT="${PROXY_PORT:-8080}"
KEEP_WORK_DIR="${KEEP_WORK_DIR:-false}"

WORK_DIR="$(mktemp -d)"
PIDS=()

cleanup() {
  local rc=$?

  for pid in "${PIDS[@]}"; do
    kill "$pid" 2> /dev/null || true
  done
  for pid in "${PIDS[@]}"; do
    wait "$pid" 2> /dev/null || true
  done

  if [ "$rc" -ne 0 ]; then
    for log in "$WORK_DIR"/*.log; do
      [ -f "$log" ] || continue
      echodate "Last lines of $log:"
      tail -n 50 "$log"
    done
  fi

  if [ "$KEEP_WORK_DIR" = "true" ]; then
    echodate "Keeping work directory $WORK_DIR"
  else
    rm -rf "$WORK_DIR"
  fi

  exit "$rc"
}
trap cleanup EXIT

ensure_kcp
ensure_kubectl

echodate "Building the proxy..."
make --no-print-directory build

echodate "Starting kcp (logs in $WORK_DIR/kcp.log)..."
(
  cd "$WORK_DIR"
  exec "$TOOLS_DIR/kcp" start \
    --root-directory="$WORK_DIR/.kcp" \
    --bind-address=127.0.0.1 \
    --secure-port="$KCP_PORT" \
    --embedded-etcd-client-port="$ETCD_CLIENT_PORT" \
    --embedded-etcd-peer-port="$ETCD_PEER_PORT"
) > "$WORK_DIR/kcp.log" 2>&1 &
PIDS+=($!)

ADMIN_KUBECONFIG="$WORK_DIR/.kcp/admin.kubeconfig"

kcp_ready() {
  [ -f "$ADMIN_KUBECONFIG" ] &&
    "$TOOLS_DIR/kubectl" --kubeconfig="$ADMIN_KUBECONFIG" get --raw /readyz > /dev/null 2>&1
}
retry 120 kcp_ready
echodate "kcp is ready."

# kubectl against the root workspace; kubectl_ws <workspace> targets a child
# workspace of root.
ROOT_URL="$("$TOOLS_DIR/kubectl" --kubeconfig="$ADMIN_KUBECONFIG" config view \
  -o jsonpath='{.clusters[?(@.name=="root")].cluster.server}')"

kubectl_root() {
  "$TOOLS_DIR/kubectl" --kubeconfig="$ADMIN_KUBECONFIG" "$@"
}

kubectl_ws() {
  local ws="$1"
  shift
  kubectl_root --server="$ROOT_URL:$ws" "$@"
}

echodate "Creating workspaces..."
kubectl_root apply -f - << 'YAML'
apiVersion: tenancy.kcp.io/v1alpha1
kind: Workspace
metadata:
  name: provider
---
apiVersion: tenancy.kcp.io/v1alpha1
kind: Workspace
metadata:
  name: consumer
YAML
kubectl_root wait --for=jsonpath='{.status.phase}'=Ready \
  workspace/provider workspace/consumer --timeout=120s

echodate "Creating the APIExport in the provider workspace..."
kubectl_ws provider apply -f - << 'YAML'
apiVersion: apis.kcp.io/v1alpha1
kind: APIResourceSchema
metadata:
  name: v1.widgets.example.com
spec:
  group: example.com
  names:
    kind: Widget
    listKind: WidgetList
    plural: widgets
    singular: widget
  scope: Namespaced
  versions:
    - name: v1
      served: true
      storage: true
      schema:
        type: object
        x-kubernetes-preserve-unknown-fields: true
---
apiVersion: apis.kcp.io/v1alpha2
kind: APIExport
metadata:
  name: example.com
spec:
  resources:
    - group: example.com
      name: widgets
      schema: v1.widgets.example.com
      storage:
        crd: {}
YAML

echodate "Binding the APIExport in the consumer workspace..."
kubectl_ws consumer apply -f - << 'YAML'
apiVersion: apis.kcp.io/v1alpha2
kind: APIBinding
metadata:
  name: example.com
spec:
  reference:
    export:
      path: root:provider
      name: example.com
YAML
kubectl_ws consumer wait --for=condition=Ready apibinding/example.com --timeout=120s

echodate "Creating a Widget in the consumer workspace..."
kubectl_ws consumer apply -f - << 'YAML'
apiVersion: example.com/v1
kind: Widget
metadata:
  name: my-widget
  namespace: default
spec:
  color: blue
YAML

CONSUMER_CLUSTER="$(kubectl_root get workspace consumer -o jsonpath='{.spec.cluster}')"
echodate "Consumer workspace is logical cluster $CONSUMER_CLUSTER."

# kcp creates an APIExportEndpointSlice named after the APIExport; the proxy
# watches it from the provider workspace.
PROXY_KUBECONFIG="$WORK_DIR/proxy.kubeconfig"
cp "$ADMIN_KUBECONFIG" "$PROXY_KUBECONFIG"
"$TOOLS_DIR/kubectl" --kubeconfig="$PROXY_KUBECONFIG" config set-cluster root --server="$ROOT_URL:provider" > /dev/null
"$TOOLS_DIR/kubectl" --kubeconfig="$PROXY_KUBECONFIG" config use-context root > /dev/null

echodate "Starting the proxy (logs in $WORK_DIR/proxy.log)..."
_output/kcp-apiexport-proxy \
  --kubeconfig="$PROXY_KUBECONFIG" \
  --apiexportendpointslice-name=example.com \
  --bind-address="127.0.0.1:$PROXY_PORT" \
  > "$WORK_DIR/proxy.log" 2>&1 &
PIDS+=($!)

PROXY_URL="http://127.0.0.1:$PROXY_PORT"

proxy_ready() {
  http_get "$PROXY_URL/readyz" > /dev/null 2>&1
}
retry 60 proxy_ready
echodate "The proxy is ready."

# expect_through_proxy <path> <string> checks that GET <path> through the
# proxy eventually succeeds and returns a body containing <string>. It
# retries because the proxy's index converges asynchronously.
expect_through_proxy() {
  local path="$1"
  local want="$2"

  check() {
    http_get "$PROXY_URL$path" 2> /dev/null | grep -q "$want"
  }

  if ! retry 30 check; then
    echodate "GET $path through the proxy did not return a body containing: $want"
    return 1
  fi
  echodate "GET $path contains: $want"
}

echodate "Checking requests through the proxy..."
expect_through_proxy "/clusters/$CONSUMER_CLUSTER/apis/example.com/v1/widgets" '"name": "my-widget"'
expect_through_proxy "/clusters/$CONSUMER_CLUSTER/apis/example.com/v1/namespaces/default/widgets/my-widget" '"color": "blue"'
expect_through_proxy "/clusters/$CONSUMER_CLUSTER/apis/apis.kcp.io/v1alpha1/apibindings" '"name": "example.com"'

if http_get "$PROXY_URL/clusters/does-not-exist/apis/example.com/v1/widgets" > /dev/null 2>&1; then
  echodate "Expected a request for an unknown logical cluster to fail."
  exit 1
fi
echodate "Request for an unknown logical cluster was rejected."

echodate "Integration test passed :-)"
