#!/usr/bin/env bash
set -x
set -o errexit
set -o nounset
set -o pipefail

PROJECT_ROOT=$(cd $(dirname "$0")/.. ; pwd)
MOCKERY_MODULE=github.com/vektra/mockery/v2@v2.53.4

# generate mocks via 'go run' under your Go 1.24 toolchain
go run $MOCKERY_MODULE --dir "${PROJECT_ROOT}"/metric                         --name Provider                  --output "${PROJECT_ROOT}"/metricproviders/mocks
go run $MOCKERY_MODULE --dir "${PROJECT_ROOT}"/utils/aws                      --name ELBv2APIClient            --output "${PROJECT_ROOT}"/utils/aws/mocks
go run $MOCKERY_MODULE --dir "${PROJECT_ROOT}"/rollout/trafficrouting         --name TrafficRoutingReconciler  --output "${PROJECT_ROOT}"/rollout/mocks
go run $MOCKERY_MODULE --dir "${PROJECT_ROOT}"/rollout/steps/plugin          --name "Resolver|StepPlugin"     --output "${PROJECT_ROOT}"/rollout/steps/plugin/mocks
go run $MOCKERY_MODULE --dir "${PROJECT_ROOT}"/rollout/steps/plugin/rpc      --name "StepPlugin"              --output "${PROJECT_ROOT}"/rollout/steps/plugin/rpc/mocks