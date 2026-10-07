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

echodate "Tidying Go modules..."
go mod tidy
go mod verify

echodate "Diffing..."
if ! git diff --exit-code -- go.mod go.sum; then
  echodate "go.mod/go.sum are not up to date. Please run 'go mod tidy'."
  exit 1
fi

echodate "Go modules are tidy."
