#! /usr/bin/env bash

set -x
set -o errexit
set -o nounset
set -o pipefail


# code-generator does work with go.mod but makes assumptions about the project living in `$GOPATH/src`. 
# To work around this and support any location: 
#   create a temporary directory, use this as an output base, and copy everything back once generated.
export GOPATH=$(go env GOPATH) # export gopath so it's available to generate scripts
SCRIPT_ROOT="$( cd "$( dirname "${BASH_SOURCE[0]}" )/.." >/dev/null 2>&1 && pwd )"
CODEGEN_VERSION=$(go list -m k8s.io/code-generator | awk '{print $NF}' | head -1)
CODEGEN_PKG="${GOPATH}/pkg/mod/k8s.io/code-generator@${CODEGEN_VERSION}"
TEMP_DIR=$(mktemp -d)
cleanup() {
    rm -rf ${TEMP_DIR}
}
trap "cleanup" EXIT SIGINT

TARGET_SCRIPT=kube_codegen.sh

chmod +x ${CODEGEN_PKG}/${TARGET_SCRIPT}

source ${CODEGEN_PKG}/${TARGET_SCRIPT}

kube::codegen::gen_helpers pkg/apis/rollouts/v1alpha1 \
  --boilerplate "${SCRIPT_ROOT}/hack/boilerplate.go.txt"

kube::codegen::gen_client pkg/apis \
  --with-watch \
  --output-pkg github.com/argoproj/argo-rollouts/pkg/client \
  --output-dir "${TEMP_DIR}" \
  --boilerplate "${SCRIPT_ROOT}/hack/boilerplate.go.txt"

cp -rf "${TEMP_DIR}/." "${SCRIPT_ROOT}/pkg/client/"
