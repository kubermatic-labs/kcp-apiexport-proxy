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

export CGO_ENABLED ?= 0
export GOFLAGS ?= -mod=readonly -trimpath

.PHONY: build
build:
	./build/build.sh

.PHONY: test
test:
	go test ./...

.PHONY: test-integration
test-integration:
	./test/test-integration.sh

.PHONY: verify
verify:
	./hack/verify-boilerplate.sh
	./hack/verify-dependencies.sh
	./hack/verify-unicode.sh
	./hack/verify-import-order.sh
	./hack/verify-lint.sh
