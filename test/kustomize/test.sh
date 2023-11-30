#!/bin/bash

PROJECT_ROOT=$(cd $(dirname ${BASH_SOURCE})/../..; pwd)
DIST_PATH="${PROJECT_ROOT}/dist"

kustomize_cmd="kustomize"
kustomize_version=v5.2.1
kustomize_major_version=v5

# check if local kustomize is installed
if test -x "${DIST_PATH}/kustomize"; then
    kustomize_cmd="${DIST_PATH}/kustomize"
    # we are unable to check local version of kustomize as it returns (devel) if installs via go install
else
    # check if kustomize in path exists and is the expected version
    install_kustomize=false
    if ! command -v kustomize &> /dev/null; then
        echo "kustomize not found in path."
        install_kustomize=true
    elif ! kustomize version | grep -q "${kustomize_version}"; then
        echo "kustomize version is not expected."
        install_kustomize=true
    fi

    # install local kustomize if needed
    if ${install_kustomize}; then
        echo "Installing local version ${kustomize_version} to ${DIST_PATH}/kustomize";
        GOBIN="${DIST_PATH}" GO111MODULE=on go install "sigs.k8s.io/kustomize/kustomize/${kustomize_major_version}@${kustomize_version}"
        kustomize_cmd="${DIST_PATH}/kustomize"
    fi
fi
echo "Using kustomize: ${kustomize_cmd}"

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null 2>&1 && pwd )"

for i in `ls ${DIR}/*/kustomization.yaml`; do
    dir_name=$(dirname $i)
    diff_out=$(diff ${dir_name}/expected.yaml <("${kustomize_cmd}" build ${dir_name} --load-restrictor=LoadRestrictionsNone))
    if [[ $? -ne 0 ]]; then
        echo "${i} had unexpected diff:"
        echo "${diff_out}"
        exit 1
    fi
done