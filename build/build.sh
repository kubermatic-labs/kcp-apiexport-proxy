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
### <command>_<version>_<os>_<arch>.tar.gz archive containing the binary,
### and a .sha256 checksum file for that archive. The version defaults to
### "git describe" and is embedded into the binary.

set -euo pipefail

cd "$(dirname "$0")/.."

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
    -ldflags "$LDFLAGS -X github.com/kcp-dev/contrib-apiexport-proxy/pkg/version.Version=$VERSION" \
    -o "$BUILD_DEST/$cmd" "./cmd/$cmd"

  archive="${cmd}_${VERSION}_${GOOS}_${GOARCH}.tar.gz"
  tar --create --gzip --owner=0 --group=0 --numeric-owner \
    --file "$BUILD_DEST/$archive" --directory "$BUILD_DEST" "$cmd"
  (cd "$BUILD_DEST" && sha256sum "$archive" > "$archive.sha256")
done
