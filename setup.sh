#!/usr/bin/env bash

# Copyright The SOPS Operator Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     https://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b "$(go env GOPATH)/bin" v1.55.2
go install github.com/goreleaser/goreleaser
go install github.com/magefile/mage
go install golang.org/x/tools/cmd/goimports
go install sigs.k8s.io/controller-tools/cmd/controller-gen
