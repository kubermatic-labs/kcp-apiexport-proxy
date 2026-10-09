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

### Builds the given commands from cmd/ (all of them by default) into
### $BUILD_DEST. For each command it also writes a
### <command>-<version>-<os>-<arch>.tar.gz archive containing the binary,
### and a .sha256 checksum file for that archive. The version defaults to
### "git describe" and is embedded into the binary.
###
### It also packages the Helm chart as
### kcp-apiexport-proxy-<version>-helm-chart.tar.gz with a .sha256 checksum
### file, using the version as chart version and
### appVersion. Chart versions must be SemVer, so a version that isn't (e.g.
### a bare commit hash) is turned into v0.0.0-<version> for the chart.

set -euo pipefail

cd "$(dirname "$0")/.."
source hack/lib.sh

BUILD_DEST="${BUILD_DEST:-_output}"
VERSION="${VERSION:-$(git describe --tags --always --dirty)}"
GOBUILDFLAGS="${GOBUILDFLAGS:--v}"
LDFLAGS="${LDFLAGS:--w -extldflags '-static'}"

if [ "$#" -eq 0 ]; then
  set -- $(ls cmd)
fi

mkdir -p "$BUILD_DEST"

GOOS="$(go env GOOS)"
GOARCH="$(go env GOARCH)"

for cmd in "$@"; do
  # shellcheck disable=SC2086
  go build $GOBUILDFLAGS \
    -ldflags "$LDFLAGS -X github.com/kubermatic-labs/kcp-apiexport-proxy/pkg/version.Version=$VERSION" \
    -o "$BUILD_DEST/$cmd" "./cmd/$cmd"

  archive="${cmd}-${VERSION}-${GOOS}-${GOARCH}.tar.gz"
  tar --create --gzip --owner=0 --group=0 --numeric-owner \
    --file "$BUILD_DEST/$archive" --directory "$BUILD_DEST" "$cmd"
  (cd "$BUILD_DEST" && sha256sum "$archive" > "$archive.sha256")
done

ensure_helm

CHART_VERSION="$VERSION"
if ! [[ "$CHART_VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+ ]]; then
  CHART_VERSION="v0.0.0-$VERSION"
fi

chart_dir="$(mktemp -d)"
"$TOOLS_DIR/helm" package deploy/charts/kcp-apiexport-proxy \
  --version "$CHART_VERSION" \
  --app-version "$VERSION" \
  --destination "$chart_dir" > /dev/null

chart="kcp-apiexport-proxy-${VERSION}-helm-chart.tar.gz"
mv "$chart_dir/kcp-apiexport-proxy-$CHART_VERSION.tgz" "$BUILD_DEST/$chart"
rm -rf "$chart_dir"
(cd "$BUILD_DEST" && sha256sum "$chart" > "$chart.sha256")
