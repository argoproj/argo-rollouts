set -o errexit
set -o nounset
set -o pipefail


# code-generator does work with go.mod but makes assumptions about the project living in `$GOPATH/src`. 
# To work around this and support any location: 
#   create a temporary directory, use this as an output base, and copy everything back once generated.

SCRIPT_ROOT=$(realpath $(dirname ${BASH_SOURCE})/..)
CODEGEN_VERSION=$(go list -m all | grep 'k8s.io/code-generator' | awk '{print $2}' | head -1)
CODEGEN_PKG=$(echo `go env GOPATH`"/pkg/mod/k8s.io/code-generator@${CODEGEN_VERSION}")

TEMP_DIR=$(mktemp -d)
cleanup() {
    rm -rf ${TEMP_DIR}
}
trap "cleanup" EXIT SIGINT


chmod +x ${CODEGEN_PKG}/generate-groups.sh

${CODEGEN_PKG}/generate-groups.sh "deepcopy,client,informer,lister" \
  github.com/argoproj/argo-rollouts/pkg/client github.com/argoproj/argo-rollouts/pkg/apis \
  "rollouts:v1alpha1" \
  --output-base "${TEMP_DIR}" \
  --go-header-file ${SCRIPT_ROOT}/hack/boilerplate.go.txt

cp -r "${TEMP_DIR}/github.com/argoproj/argo-rollouts/." "${SCRIPT_ROOT}/"
# To use your own boilerplate text use:
#   --go-header-file ${SCRIPT_ROOT}/hack/custom-boilerplate.go.txt
