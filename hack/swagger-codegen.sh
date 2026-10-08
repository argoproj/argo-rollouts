#!/bin/bash
set -o errexit
set -o nounset
set -o pipefail

export SWAGGER_CODEGEN_VERSION=3.0.54
PROJECT_ROOT=$(cd $(dirname ${BASH_SOURCE})/..; pwd)

# dist/ is gitignored, so it does not exist on a fresh clone.
mkdir -p "$PROJECT_ROOT/dist"

test -f "$PROJECT_ROOT/dist/swagger-codegen-cli-${SWAGGER_CODEGEN_VERSION}.jar" || \
    curl --fail --show-error https://repo1.maven.org/maven2/io/swagger/codegen/v3/swagger-codegen-cli/${SWAGGER_CODEGEN_VERSION}/swagger-codegen-cli-${SWAGGER_CODEGEN_VERSION}.jar -o "$PROJECT_ROOT/dist/swagger-codegen-cli-${SWAGGER_CODEGEN_VERSION}.jar"

docker run --rm -v "$PROJECT_ROOT:/src" -w /src/ui -t maven:3-jdk-11-slim java -jar /src/dist/swagger-codegen-cli-${SWAGGER_CODEGEN_VERSION}.jar "$@"
