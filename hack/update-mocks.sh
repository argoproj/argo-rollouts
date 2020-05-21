#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail

source $(dirname $0)/library.sh

header "updating mock files"
ensure_vendor

cd ${REPO_ROOT}

MOCKERY_PKG="${REPO_ROOT}/vendor/github.com/vektra/mockery"
go run "${MOCKERY_PKG}"/cmd/mockery \
    -dir "${REPO_ROOT}"/metricproviders \
    -name Provider \
    -output "${REPO_ROOT}"/metricproviders/mocks

