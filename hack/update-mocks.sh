#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail

PROJECT_ROOT=$(cd $(dirname "$0")/.. ; pwd)
MOCKERY_VERSION=$(go list -m github.com/vektra/mockery | awk '{print $2}' | head -1)
MOCKERY_PKG=$(echo `go env GOPATH`"/pkg/mod/github.com/vektra/mockery@${MOCKERY_VERSION}")


go run "${MOCKERY_PKG}"/cmd/mockery/mockery.go \
    -dir "${PROJECT_ROOT}"/metricproviders \
    -name Provider \
    -output "${PROJECT_ROOT}"/metricproviders/mocks

