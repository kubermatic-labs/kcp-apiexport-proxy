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

status=0
matches=$(LC_ALL=C git grep --untracked -nI '[^ -~	]' -- . ':!LICENSE') || status=$?

# git grep exits 1 when it finds nothing; anything higher is a real error.
if [ "$status" -gt 1 ]; then
  echo "error: git grep failed with exit code $status" >&2
  exit "$status"
fi

if [ -n "$matches" ]; then
  echo "$matches"
  echo ""
  echo "Error: Some files contain unicode characters. Please remove them!"
  exit 1
fi

echo "No unicode characters found."
