# Argo Rollouts - Advanced Kubernetes Deployment Controller

## What is Argo Rollouts?
Argo Rollouts introduces a new custom resource called a Rollout to provide additional deployment strategies such as Blue Green and Canary to Kubernetes. The Rollout custom resource provides feature parity with the deployment resource with additional deployment strategies. Check out the [Deployment Concepts](#deployment-concepts) for more information on the various deployment strategies.

## Why Argo Rollouts?
Deployments resources offer two strategies to deploy changes: `RollingUpdate` and `Recreate`. While these strategies can solve a wide number of use cases, large scale production deployments use additional strategies, such as blue-green or canary, that are missing from the Deployment controller. In order to use these strategies in Kubernetes, users are forced to build scripts on top of their deployments. The Argo Rollouts controller provides these strategies as simple declarative, configurable, GitOps-friendly options.

## Getting Started

### Quick Start

Argo Rollouts can be installed by running the following commands:
```
$ kubectl create namespace argo-rollouts
$ kubectl apply -n argo-rollouts -f https://raw.githubusercontent.com/argoproj/argo-rollout/tree/stable/manifests/install.yaml
```

Check out the [getting started guide](getting-started.md) to walk through creating and then updating a rollout object. 

### How does it work?
Similar to the deployment object, the Argo Rollouts controller will manage the creation, scaling, and deletion of ReplicaSets. These ReplicaSets are defined by the `spec.template` field, which uses the same pod template as the deployment object. When the `spec.template` is changed, that signals to the Argo Rollouts controller that a new ReplicaSet will be introduced. The controller will use the strategy set within the `spec.strategy` field in order to determine how the rollout will progress from the old ReplicaSet to the new ReplicaSet. Once that new ReplicaSet has successfully progressed into the stable version, that Rollout will be marked as the stable ReplicaSet. If another change occurs in the `spec.template` during a transition from a stable ReplicaSet to a new ReplicaSet. The previously new ReplicaSet will be scaled down, and the controller will try to progress the ReplicasSet that reflects the `spec.template` field. There is more information on the behaviors of each strategy in the [spec](spec/) section.

### Use cases of Argo Rollouts

- A user wants to run last minute functional tests on the new version before it starts to serve production traffic. With the BlueGreen strategy, Argo Rollouts allow users to specify a preview service and an active service. The Rollout will configure the preview service to send traffic to the new version while the active service continues to receive production traffic. Once a user is satisfied, they can promote the preview service to be the new active service. ([example](https://github.com/argoproj/argo-rollouts/tree/stable/examples/example-rollout-bluegreen.yaml))

- Before a new version starts receiving live traffic, a generic set of steps need to be executed beforehand. With the BlueGreen Strategy, the user can bring up the new version without it receiving traffic from the active service. Once those steps finish executing, the rollout can cut over traffic to the new version.

- A user wants to give a small percentage of the production traffic to a new version of their application for a couple of hours. Afterward, they want to scale down the new version and look at some metrics to determine if the new version is performant compared to the old version. Then they will decide if they want to rollout the new version for all of the production traffic or stick with the current version. With the canary strategy, the rollout can scale up a replica with the new version to receive a specified percentage of traffic, wait for a specified amount of time, set the percentage back to 0, and then wait to rollout out to service all of the traffic once the user is satisfied. ([example](https://github.com/argoproj/argo-rollouts/tree/stable/examples/example-rollout-canary-run-tmp-canary.yaml))

- A user wants to slowly give the new version more production traffic. They start by giving it a small percentage of the live traffic and wait a while before giving the new version more traffic. Eventually, the new version will receive all the production traffic. With the canary strategy, the user specifies the percentages they want the new version to receive and the amount of time to wait between percentages. ([example](https://github.com/argoproj/argo-rollouts/tree/stable/examples/example-rollout-canary.yaml))

- A user wants to use the normal Rolling Update strategy from the deployment. If a user uses the canary strategy with no steps, the rollout will use the max surge and max unavailable values to roll to the new version. ([example](https://github.com/argoproj/argo-rollouts/tree/stable/examples/example-rollout-canary-rolling-update.yaml))