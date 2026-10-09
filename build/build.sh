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
### $BUILD_DEST and writes a <command>.sha256 checksum file next to each
### binary.

set -euo pipefail

cd "$(dirname "$0")/.."

BUILD_DEST="${BUILD_DEST:-_output}"
GOBUILDFLAGS="${GOBUILDFLAGS:--v}"
LDFLAGS="${LDFLAGS:--w -extldflags '-static'}"

if [ "$#" -eq 0 ]; then
  set -- $(ls cmd)
fi

mkdir -p "$BUILD_DEST"

for cmd in "$@"; do
  # shellcheck disable=SC2086
  go build $GOBUILDFLAGS -ldflags "$LDFLAGS" -o "$BUILD_DEST/$cmd" "./cmd/$cmd"
  (cd "$BUILD_DEST" && sha256sum "$cmd" > "$cmd.sha256")
done
