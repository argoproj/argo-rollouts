
# Argo Rollouts - Advanced Kubernetes Deployment Controller
[![codecov](https://codecov.io/gh/argoproj/argo-rollouts/branch/master/graph/badge.svg)](https://codecov.io/gh/argoproj/argo-rollouts)
[![slack](https://img.shields.io/badge/slack-argoproj-brightgreen.svg?logo=slack)](https://argoproj.github.io/community/join-slack)

## What is Argo Rollouts?
Argo Rollouts controller, uses the Rollout custom resource to provide additional deployment strategies such as Blue Green and Canary to Kubernetes.  The Rollout custom resource provides feature parity with the deployment resource but with additional deployment strategies.

## Why use Argo Rollouts?
Deployments resources offer two strategies to deploy changes: `RollingUpdate` and `Recreate`. While these strategies can solve a wide number of use cases, large scale production deployments use additional strategies, such as blue-green or canary, that are missing from the Deployment controller.  In order to use these strategies in Kubernetes, users are forced to build scripts on top of their deployments. The Argo Rollouts controller provides these strategies as simple declarative, configurable options.

## Documentation
To learn more about Argo CD go to the [complete documentation](https://argoproj.github.io/argo-rollouts/).