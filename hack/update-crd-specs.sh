#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail

source $(dirname $0)/library.sh

header "updating crd specs"

if [ "`command -v controller-gen`" = "" ]; then
  go get sigs.k8s.io/controller-tools/cmd/controller-gen@v0.2.5
fi

go run ./hack/gen-crd-spec/main.go

