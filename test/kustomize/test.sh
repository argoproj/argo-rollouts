#!/bin/bash

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null 2>&1 && pwd )"

for i in `ls ${DIR}/*/kustomization.yaml`; do
    dir_name=$(dirname $i)
    diff_out=$(diff ${dir_name}/expected.yaml <(kustomize build ${dir_name} --load-restrictor=LoadRestrictionsNone))
    if [[ $? -ne 0 ]]; then
        echo "${i} had unexpected diff:"
        echo "${diff_out}"
        exit 1
    fi
done