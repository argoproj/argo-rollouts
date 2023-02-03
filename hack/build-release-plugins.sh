#!/bin/sh

set -xe

SRCROOT="$( CDPATH='' cd -- "$(dirname "$0")/.." && pwd -P )"
mkdir -p ${SRCROOT}/dist

rollout_iid_file=$(mktemp -d "${SRCROOT}/dist/rollout_iid.XXXXXXXXX")
DOCKER_BUILDKIT=1 docker build --iidfile ${rollout_iid_file} --build-arg MAKE_TARGET="plugin-linux plugin-darwin plugin-windows" \
--target argo-rollouts-build .
rollout_iid=$(cat ${rollout_iid_file})
container_id=$(docker create ${rollout_iid})

for plat in linux-amd64 linux-arm64 darwin-amd64 darwin-arm64 windows-amd64; do
    docker cp ${container_id}:/go/src/github.com/argoproj/argo-rollouts/dist/kubectl-argo-rollouts-${plat} ${SRCROOT}/dist
done

docker rm -v ${container_id}
rm -f ${rollout_iid_file}
