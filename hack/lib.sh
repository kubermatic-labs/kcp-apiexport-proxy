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
KIND_VERSION="0.33.0"
KIND_NODE_IMAGE="kindest/node:v1.36.4@sha256:099e049362a1526b2db71494e1947aae99bd16290d7c895f2b7ea312e3cbfaed"
KUBECTL_VERSION="1.36.5"
KYVERNO_VERSION="1.19.1"

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

# http_get writes the contents of a URL to stdout, using curl if available
# and falling back to wget otherwise. It fails on HTTP error responses.
http_get() {
  local url="$1"

  if command -v curl > /dev/null 2>&1; then
    curl --fail --silent --show-error --location "$url"
  elif command -v wget > /dev/null 2>&1; then
    wget --quiet --output-document=- "$url"
  else
    echodate "Neither curl nor wget is installed, please install one of them." >&2
    return 1
  fi
}

# install_tool downloads a release binary into $TOOLS_DIR as <name>-<version>
# and symlinks <name> to it. Nothing is downloaded if that version is already
# present, so bumping a pinned version above is all it takes to update a tool.
#
# usage: install_tool <name> <version> <url> [path of the binary inside the tarball]
#
# If the last argument is omitted, the URL is expected to point at the binary
# itself rather than at a .tar.gz archive.
install_tool() {
  local name="$1"
  local version="$2"
  local url="$3"
  local member="${4:-}"
  local target="$TOOLS_DIR/$name-$version"

  if [ ! -x "$target" ]; then
    echodate "Downloading $name $version ..."
    mkdir -p "$TOOLS_DIR"
    local tmp
    tmp="$(mktemp -d)"
    if [ -n "$member" ]; then
      http_get "$url" | tar -xz -C "$tmp" "$member"
    else
      member="$name"
      http_get "$url" > "$tmp/$member"
    fi
    chmod +x "$tmp/$member"
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

ensure_kind() {
  install_tool kind "$KIND_VERSION" \
    "https://github.com/kubernetes-sigs/kind/releases/download/v${KIND_VERSION}/kind-${OS}-${ARCH}"
}

ensure_kubectl() {
  install_tool kubectl "$KUBECTL_VERSION" \
    "https://dl.k8s.io/release/v${KUBECTL_VERSION}/bin/${OS}/${ARCH}/kubectl"
}

# retry runs the given command until it succeeds, up to the given number of
# attempts with a two second pause in between.
#
# usage: retry <attempts> <command...>
retry() {
  local attempts="$1"
  shift

  local i
  for ((i = 1; i <= attempts; i++)); do
    if "$@"; then
      return 0
    fi
    sleep 2
  done

  echodate "Command failed after $attempts attempts: $*" >&2
  return 1
}
