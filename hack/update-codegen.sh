set -o errexit
set -o nounset
set -o pipefail
SCRIPT_ROOT=$(dirname ${BASH_SOURCE})/..
CODEGEN_PKG=${CODEGEN_PKG:-$(cd ${SCRIPT_ROOT}; ls -d -1 ./vendor/k8s.io/code-generator 2>/dev/null || echo ../code-generator)}

${CODEGEN_PKG}/generate-groups.sh "deepcopy,client,informer,lister" \
  github.com/argoproj/argo-rollouts/pkg/client github.com/argoproj/argo-rollouts/pkg/apis \
  rollouts:v1alpha1 \
  --go-header-file ${SCRIPT_ROOT}/hack/boilerplate.go.txt

# To use your own boilerplate text use:
#   --go-header-file ${SCRIPT_ROOT}/hack/custom-boilerplate.go.txt
