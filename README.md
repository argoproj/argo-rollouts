
# Argo Rollouts - Progressive Delivery for Kubernetes
[![codecov](https://codecov.io/gh/argoproj/argo-rollouts/branch/master/graph/badge.svg)](https://codecov.io/gh/argoproj/argo-rollouts)
[![slack](https://img.shields.io/badge/slack-argoproj-brightgreen.svg?logo=slack)](https://argoproj.github.io/community/join-slack)

## What is Argo Rollouts?
Argo Rollouts controller, uses the Rollout custom resource to provide additional deployment strategies such as Blue Green and Canary to Kubernetes.  The Rollout custom resource provides feature parity with the deployment resource but with additional deployment strategies.

## Why use Argo Rollouts?
Deployments resources offer two strategies to deploy changes: `RollingUpdate` and `Recreate`. While these strategies can solve a wide number of use cases, large scale production deployments use additional strategies, such as blue-green or canary, that are missing from the Deployment controller.  In order to use these strategies in Kubernetes, users are forced to build scripts on top of their deployments. The Argo Rollouts controller provides these strategies as simple declarative, configurable options.

## Documentation
To learn more about Argo Rollouts go to the [complete documentation](https://argoproj.github.io/argo-rollouts/).

## Who uses Argo Rollouts?
Organizations below are **officially** using Argo Rollouts. Please send a PR with your organization name if you are using Argo Rollouts.

1. [ADP](https://www.adp.com)
1. [Intuit](https://www.intuit.com/)

## Community Blogs and Presentations
* [How Intuit Does Canary and Blue Green Deployments](https://www.youtube.com/watch?v=yeVkTTO9nOA)
* [Leveling Up Your CD: Unlocking Progressive Delivery on Kubernetes](https://www.youtube.com/watch?v=Nv0PPwbIEkY)
