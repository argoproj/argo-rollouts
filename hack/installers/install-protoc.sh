#!/bin/bash
set -eux -o pipefail

PROJECT_ROOT=$(cd $(dirname ${BASH_SOURCE})/../..; pwd)
DIST_PATH="${PROJECT_ROOT}/dist"
PATH="${DIST_PATH}:${PATH}"

protoc_version=3.17.3

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

export TARGET_FILE=protoc_${protoc_version}_${OS}_${ARCH}.zip
temp_path="/tmp/${TARGET_FILE}"
url=https://github.com/protocolbuffers/protobuf/releases/download/v${protoc_version}/protoc-${protoc_version}-${protoc_os}-${protoc_arch}.zip
[ -e ${temp_path} ] || curl -sLf --retry 3 -o ${temp_path} ${url}

mkdir -p /tmp/protoc-${protoc_version}
unzip -o ${temp_path} -d /tmp/protoc-${protoc_version}
mkdir -p ${DIST_PATH}/protoc-include
cp /tmp/protoc-${protoc_version}/bin/protoc ${DIST_PATH}/protoc
chmod +x ${DIST_PATH}/protoc
cp -a /tmp/protoc-${protoc_version}/include/* ${DIST_PATH}/protoc-include
chmod -R +rx ${DIST_PATH}/protoc-include
protoc --version
