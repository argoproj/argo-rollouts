#!/bin/bash

set -x
set -o errexit
set -o nounset
set -o pipefail

PROJECT_ROOT=$(cd $(dirname "$0")/.. ; pwd)

mockery \
    --dir "${PROJECT_ROOT}"/metric \
    --name Provider \
    --output "${PROJECT_ROOT}"/metricproviders/mocks

mockery \
    --dir "${PROJECT_ROOT}"/utils/aws \
    --name ELBv2APIClient \
    --output "${PROJECT_ROOT}"/utils/aws/mocks

mockery \
    --dir "${PROJECT_ROOT}"/rollout/trafficrouting \
    --name TrafficRoutingReconciler \
    --output "${PROJECT_ROOT}"/rollout/mocks

mockery \
    --dir "${PROJECT_ROOT}"/rollout/steps/plugin \
    --name "Resolver|StepPlugin" \
    --output "${PROJECT_ROOT}"/rollout/steps/plugin/mocks

mockery \
    --dir "${PROJECT_ROOT}"/rollout/steps/plugin/rpc \
    --name "StepPlugin" \
    --output "${PROJECT_ROOT}"/rollout/steps/plugin/rpc/mocks
