#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail

export GOPATH=$(go env GOPATH) # export gopath do generator output goes to the correct location
PROJECT_ROOT=$(cd $(dirname "$0")/.. ; pwd)
CODEGEN_VERSION=$(go list -m k8s.io/kube-openapi | awk '{print $2}' | head -1)
CODEGEN_PKG="${GOPATH}/pkg/mod/k8s.io/kube-openapi@${CODEGEN_VERSION}"
VERSION="v1alpha1"

go run ${CODEGEN_PKG}/cmd/openapi-gen/openapi-gen.go \
  --go-header-file ${PROJECT_ROOT}/hack/custom-boilerplate.go.txt \
  --input-dirs github.com/argoproj/argo-rollouts/pkg/apis/rollouts/${VERSION} \
  --output-package github.com/argoproj/argo-rollouts/pkg/apis/rollouts/${VERSION} \
  --report-filename pkg/apis/api-rules/violation_exceptions.list \
  $@

