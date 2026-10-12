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

### Creates the GitHub Release for a tag, with the linux/amd64 binary and the
### Helm chart (built by build/build.sh) and their .sha256 files as assets.
### Re-running it for an existing release replaces the assets.
###
### It is run by the post-kcp-apiexport-proxy-github-release Prow job (see
### kubermatic/infra), which provides the tag in $PULL_BASE_REF, the repository
### in $REPO_OWNER/$REPO_NAME and a GitHub token in
### $KUBERMATIC_BOT_GITHUB_TOKEN. Outside of Prow, set these yourself.

set -euo pipefail

cd "$(dirname "$0")/.."
source hack/lib.sh

VERSION="${PULL_BASE_REF:?PULL_BASE_REF must be set to the tag to release}"
REPOSITORY="${REPO_OWNER:?REPO_OWNER must be set}/${REPO_NAME:?REPO_NAME must be set}"
export GH_TOKEN="${KUBERMATIC_BOT_GITHUB_TOKEN:?KUBERMATIC_BOT_GITHUB_TOKEN must be set}"

if ! [[ "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+ ]]; then
  echodate "$VERSION is not a release tag (vX.Y.Z...)."
  exit 1
fi

ensure_gh

BUILD_DEST="$ROOT_DIR/_output/release"

echodate "Building $VERSION..."
BUILD_DEST="$BUILD_DEST" VERSION="$VERSION" GOOS=linux GOARCH=amd64 ./build/build.sh

assets=(
  "$BUILD_DEST/kcp-apiexport-proxy-$VERSION-linux-amd64.tar.gz"
  "$BUILD_DEST/kcp-apiexport-proxy-$VERSION-linux-amd64.tar.gz.sha256"
  "$BUILD_DEST/kcp-apiexport-proxy-$VERSION-helm-chart.tar.gz"
  "$BUILD_DEST/kcp-apiexport-proxy-$VERSION-helm-chart.tar.gz.sha256"
)

if "$TOOLS_DIR/gh" release view "$VERSION" --repo "$REPOSITORY" > /dev/null 2>&1; then
  echodate "Release $VERSION exists, replacing its assets..."
  "$TOOLS_DIR/gh" release upload "$VERSION" --repo "$REPOSITORY" --clobber "${assets[@]}"
else
  echodate "Creating release $VERSION..."
  "$TOOLS_DIR/gh" release create "$VERSION" --repo "$REPOSITORY" \
    --verify-tag --generate-notes "${assets[@]}"
fi

echodate "Released $VERSION."
