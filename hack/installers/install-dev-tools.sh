#!/bin/bash
set -eux -o pipefail

PROJECT_ROOT=$(cd $(dirname ${BASH_SOURCE})/../..; pwd)
DIST_PATH="${PROJECT_ROOT}/dist"
PATH="${DIST_PATH}:${PATH}"

mkdir -p ${DIST_PATH}

gotestsum_version=1.10.1

OS=$(go env GOOS)
ARCH=$(go env GOARCH)

export TARGET_FILE=gotestsum_${gotestsum_version}_${OS}_${ARCH}.tar.gz
temp_path="/tmp/${TARGET_FILE}"
url=https://github.com/gotestyourself/gotestsum/releases/download/v${gotestsum_version}/gotestsum_${gotestsum_version}_${OS}_${ARCH}.tar.gz
[ -e ${temp_path} ] || curl -sLf --retry 3 -o ${temp_path} ${url}

mkdir -p /tmp/gotestsum-${gotestsum_version}
tar -xvzf ${temp_path} -C /tmp/gotestsum-${gotestsum_version}
cp /tmp/gotestsum-${gotestsum_version}/gotestsum ${DIST_PATH}/gotestsum
chmod +x ${DIST_PATH}/gotestsum
gotestsum --version
