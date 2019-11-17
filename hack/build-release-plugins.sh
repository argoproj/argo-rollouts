#!/bin/sh

set -xe

ROOT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )/.." >/dev/null 2>&1 && pwd )"
mkdir -p ${ROOT_DIR}/dist

rollout_iid_file=$(mktemp)
docker build --iidfile ${rollout_iid_file} --target argo-rollouts-build .
rollout_iid=$(cat ${rollout_iid_file})
container_id=$(docker create ${rollout_iid})

for plat in linux-amd64 darwin-amd64 ; do
    docker cp ${container_id}:/go/src/github.com/argoproj/argo-rollouts/dist/kubectl-argo-rollouts-${plat} ${ROOT_DIR}/dist
done
docker rm -v ${container_id}
rm -f ${rollout_iid_file}