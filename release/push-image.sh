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

### Builds the linux/amd64 image for a tag and pushes it as $IMAGE:<tag>
### (default quay.io/kubermatic-labs/kcp-apiexport-proxy), labelled with the
### source repository.
###
### It is run by the post-kcp-apiexport-proxy-push-image Prow job (see
### .prow/postsubmits.yaml), which provides the tag in $PULL_BASE_REF, the
### repository in $REPO_OWNER/$REPO_NAME and the Quay credentials in
### $QUAY_IO_USERNAME and $QUAY_IO_PASSWORD. Outside of Prow, set these
### yourself; Docker must then already be running.

set -euo pipefail

cd "$(dirname "$0")/.."
source hack/lib.sh

VERSION="${PULL_BASE_REF:?PULL_BASE_REF must be set to the tag to release}"
REPOSITORY="${REPO_OWNER:?REPO_OWNER must be set}/${REPO_NAME:?REPO_NAME must be set}"
IMAGE="${IMAGE:-quay.io/kubermatic-labs/kcp-apiexport-proxy}"
: "${QUAY_IO_USERNAME:?QUAY_IO_USERNAME must be set}"
: "${QUAY_IO_PASSWORD:?QUAY_IO_PASSWORD must be set}"

if ! [[ "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+ ]]; then
  echodate "$VERSION is not a release tag (vX.Y.Z...)."
  exit 1
fi

start_docker_daemon_ci

BUILD_DEST="$ROOT_DIR/_output/image-release"

echodate "Building $VERSION..."
BUILD_DEST="$BUILD_DEST" VERSION="$VERSION" GOOS=linux GOARCH=amd64 ./build/build.sh

echodate "Building $IMAGE:$VERSION..."
docker build \
  --platform linux/amd64 \
  --file build/Dockerfile \
  --label "org.opencontainers.image.source=https://github.com/$REPOSITORY" \
  --tag "$IMAGE:$VERSION" \
  "$BUILD_DEST"

registry="${IMAGE%%/*}"

# docker_login reads the password from stdin, so it must be piped in again on
# every attempt.
docker_login() {
  echo "$QUAY_IO_PASSWORD" | docker login "$registry" --username "$QUAY_IO_USERNAME" --password-stdin
}

echodate "Logging into $registry..."
retry 5 docker_login

echodate "Pushing $IMAGE:$VERSION..."
retry 5 docker push "$IMAGE:$VERSION"

echodate "Pushed $IMAGE:$VERSION."
