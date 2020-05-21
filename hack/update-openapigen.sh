#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail

source $(dirname $0)/library.sh

header "running openapigen"

ensure_vendor
make_fake_paths

export GOPATH="${FAKE_GOPATH}"
export GO111MODULE="off"

CODEGEN_PKG=${FAKE_REPOPATH}/vendor/k8s.io/kube-openapi
VERSION="v1alpha1"

cd "${FAKE_REPOPATH}"

go run ${CODEGEN_PKG}/cmd/openapi-gen/openapi-gen.go \
  --go-header-file ${REPO_ROOT}/hack/custom-boilerplate.go.txt \
  --input-dirs github.com/argoproj/argo-rollouts/pkg/apis/rollouts/${VERSION} \
  --output-package github.com/argoproj/argo-rollouts/pkg/apis/rollouts/${VERSION} \
  --report-filename pkg/apis/api-rules/violation_exceptions.list \
  $@

