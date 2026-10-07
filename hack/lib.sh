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

# This file is meant to be sourced by the other scripts in hack/.

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TOOLS_DIR="${TOOLS_DIR:-$ROOT_DIR/_output/tools}"

# Pinned tool versions.
BOILERPLATE_VERSION="0.3.0"
GIMPS_VERSION="0.6.2"
GOLANGCI_LINT_VERSION="2.14.0"

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64) ARCH=amd64 ;;
  aarch64) ARCH=arm64 ;;
esac

echodate() {
  # do not use -Is to keep this compatible with macOS
  echo "[$(date +%Y-%m-%dT%H:%M:%S%z)]" "$@"
}

# install_tool downloads a release tarball and extracts a single binary from
# it into $TOOLS_DIR as <name>-<version>, then symlinks <name> to it. Nothing
# is downloaded if that version is already present.
#
# usage: install_tool <name> <version> <url> <path of the binary inside the tarball>
install_tool() {
  local name="$1"
  local version="$2"
  local url="$3"
  local member="$4"
  local target="$TOOLS_DIR/$name-$version"

  if [ ! -x "$target" ]; then
    echodate "Downloading $name $version ..."
    mkdir -p "$TOOLS_DIR"
    local tmp
    tmp="$(mktemp -d)"
    curl --fail --silent --show-error --location "$url" | tar -xz -C "$tmp" "$member"
    mv "$tmp/$member" "$target"
    rm -rf "$tmp"
  fi

  ln -sf "$name-$version" "$TOOLS_DIR/$name"
}

ensure_boilerplate() {
  install_tool boilerplate "$BOILERPLATE_VERSION" \
    "https://github.com/kubermatic-labs/boilerplate/releases/download/v${BOILERPLATE_VERSION}/boilerplate_${BOILERPLATE_VERSION}_${OS}_${ARCH}.tar.gz" \
    "boilerplate_${BOILERPLATE_VERSION}_${OS}_${ARCH}/boilerplate"
}

ensure_gimps() {
  install_tool gimps "$GIMPS_VERSION" \
    "https://github.com/xrstf/gimps/releases/download/v${GIMPS_VERSION}/gimps_${GIMPS_VERSION}_${OS}_${ARCH}.tar.gz" \
    "gimps_${GIMPS_VERSION}_${OS}_${ARCH}/gimps"
}

ensure_golangci_lint() {
  install_tool golangci-lint "$GOLANGCI_LINT_VERSION" \
    "https://github.com/golangci/golangci-lint/releases/download/v${GOLANGCI_LINT_VERSION}/golangci-lint-${GOLANGCI_LINT_VERSION}-${OS}-${ARCH}.tar.gz" \
    "golangci-lint-${GOLANGCI_LINT_VERSION}-${OS}-${ARCH}/golangci-lint"
}
