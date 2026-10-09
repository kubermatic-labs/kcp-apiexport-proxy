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

### Packages the Helm chart for the given version (a "v"-prefixed git tag,
### e.g. v0.1.0) into _output/chart and pushes it to $CHART_REPOSITORY. The
### chart version is the tag without the "v", the appVersion (and thus the
### default image tag) is the tag itself. Set NO_PUSH to only package it.
###
### Pushing logs into the registry with $REGISTRY_USERNAME and
### $REGISTRY_PASSWORD.

set -euo pipefail

cd "$(dirname "$0")/.."
source hack/lib.sh

if [ "$#" -ne 1 ]; then
  echo "Usage: $(basename "$0") <version>" >&2
  exit 1
fi

VERSION="$1"
CHART_VERSION="${VERSION#v}"
CHART_REPOSITORY="${CHART_REPOSITORY:-oci://ghcr.io/kubermatic-labs/charts}"
CHART_DIR="_output/chart"

ensure_helm

rm -rf "$CHART_DIR"
"$TOOLS_DIR/helm" package deploy/charts/kcp-apiexport-proxy \
  --version "$CHART_VERSION" \
  --app-version "$VERSION" \
  --destination "$CHART_DIR"

if [ -n "${NO_PUSH:-}" ]; then
  echodate "Not pushing the chart because \$NO_PUSH is set."
  exit 0
fi

registry="${CHART_REPOSITORY#oci://}"
registry="${registry%%/*}"
echo "$REGISTRY_PASSWORD" | "$TOOLS_DIR/helm" registry login "$registry" \
  --username "$REGISTRY_USERNAME" --password-stdin

"$TOOLS_DIR/helm" push "$CHART_DIR/kcp-apiexport-proxy-$CHART_VERSION.tgz" "$CHART_REPOSITORY"
