#!/bin/bash

set -x
set -o errexit
set -o nounset
set -o pipefail

PROJECT_ROOT=$(cd $(dirname "$0")/.. ; pwd)
MOCKERY_VERSION=$(go list -m github.com/vektra/mockery/v2 | awk '{print $2}' | head -1)
MOCKERY_PKG=$(echo `go env GOPATH`"/pkg/mod/github.com/vektra/mockery/v2@${MOCKERY_VERSION}")

go run "${MOCKERY_PKG}"/main.go \
    --dir "${PROJECT_ROOT}"/metricproviders \
    --name Provider \
    --output "${PROJECT_ROOT}"/metricproviders/mocks

go run "${MOCKERY_PKG}"/main.go \
    --dir "${PROJECT_ROOT}"/utils/aws \
    --name ELBv2APIClient \
    --output "${PROJECT_ROOT}"/utils/aws/mocks

go run "${MOCKERY_PKG}"/main.go \
    --dir "${PROJECT_ROOT}"/rollout \
    --name TrafficRoutingReconciler \
    --output "${PROJECT_ROOT}"/rollout/mocks
