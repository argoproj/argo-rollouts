#!/bin/bash

export SWAGGER_CODEGEN_VERSION=3.0.25
PROJECT_ROOT=$(cd $(dirname ${BASH_SOURCE})/..; pwd)

test -f "/tmp/swagger-codegen-cli-${SWAGGER_CODEGEN_VERSION}.jar" || \
    curl https://repo1.maven.org/maven2/io/swagger/codegen/v3/swagger-codegen-cli/${SWAGGER_CODEGEN_VERSION}/swagger-codegen-cli-${SWAGGER_CODEGEN_VERSION}.jar -o "/tmp/swagger-codegen-cli-${SWAGGER_CODEGEN_VERSION}.jar"

docker run --rm -v /tmp:/tmp -v $PROJECT_ROOT:/src -w /src/ui -t maven:3-jdk-8 java -jar /tmp/swagger-codegen-cli-${SWAGGER_CODEGEN_VERSION}.jar $@