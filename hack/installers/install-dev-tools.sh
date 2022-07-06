#!/bin/bash
set -eux -o pipefail

PROJECT_ROOT=$(cd $(dirname ${BASH_SOURCE})/../..; pwd)
DIST_PATH="${PROJECT_ROOT}/dist"
PATH="${DIST_PATH}:${PATH}"

mkdir -p ${DIST_PATH}

gotestsum_version=1.8.1

OS=$(go env GOOS)
ARCH=$(go env GOARCH)
case $OS in
  darwin)
    # For macOS, the x86_64 binary is used even on Apple Silicon (it is run through rosetta), so
    # we download and install the x86_64 version. See: https://github.com/protocolbuffers/protobuf/pull/8557
    protoc_os=osx
    protoc_arch=x86_64
    ;;
  *)
    protoc_os=linux
    case $ARCH in
      arm64|arm)
        protoc_arch=aarch_64
        ;;
      *)
        protoc_arch=x86_64
        ;;
    esac
    ;;
esac

export TARGET_FILE=protoc_${gotestsum_version}_${OS}_${ARCH}.tar.gz
temp_path="/tmp/${TARGET_FILE}"
url=https://github.com/gotestyourself/gotestsum/releases/download/v${gotestsum_version}/gotestsum_${gotestsum_version}_${OS}_${ARCH}.tar.gz
[ -e ${temp_path} ] || curl -sLf --retry 3 -o ${temp_path} ${url}

mkdir -p /tmp/gotestsum-${gotestsum_version}
tar -xvzf ${temp_path} -C /tmp/gotestsum-${gotestsum_version}
cp /tmp/gotestsum-${gotestsum_version}/gotestsum ${DIST_PATH}/gotestsum
chmod +x ${DIST_PATH}/gotestsum
gotestsum --version
