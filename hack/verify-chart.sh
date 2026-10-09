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

set -euo pipefail

cd "$(dirname "$0")/.."
source hack/lib.sh

ensure_helm

CHART=deploy/charts/kcp-apiexport-proxy
REQUIRED=(--set 'apiExportEndpointSliceNames={example}' --set kubeconfig.secretName=kubeconfig)

echodate "Linting the Helm chart..."
"$TOOLS_DIR/helm" lint --strict "$CHART" "${REQUIRED[@]}"

echodate "Rendering the Helm chart with and without a token and TLS..."
for token in true false; do
  for tls in true false; do
    "$TOOLS_DIR/helm" template "$CHART" "${REQUIRED[@]}" --set token.enabled=$token --set tls.enabled=$tls > /dev/null
  done
done

echodate "The Helm chart is valid."
