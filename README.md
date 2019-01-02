
# Rollout-Controller - Advanced Kubernetes Deployment Controller
[![codecov](https://codecov.io/gh/argoproj/rollout-controller/branch/master/graph/badge.svg)](https://codecov.io/gh/argoproj/rollout-controller)
[![slack](https://img.shields.io/badge/slack-argoproj-brightgreen.svg?logo=slack)](https://argoproj.github.io/community/join-slack)

## What is the rollout-controller?
Rollout-controller intents to replace deployments by providing the same functionality as deployments along with more strategies like Blue Green and Canary

## Why the rollout-controller
Deployments resources offers two strategies to deploy changes: RollingUpdate and Recreate.  While these strategies can solve a wide number of use-cases, they are missing industry standards like blue green or canary.  In order to provide these strategies in Kubernetes, users are forced to build scripts on top of their deployments to replicate their intended behavior.  Instead of having the users worried about their scripts, the rollout controller provides these strategies as configurable options.  

## Spec
One of the design considerations of the rollout resource is making the transition from a deployment to a rollout painless.  In service to that goal, the rollout spec has the same fields as a deployment spec.  However, the strategy field in the rollout spec has additional options available like `BlueGreenUpdate` or `Canary`.  As a result, a user who wants to move from a deployment will change the `apiVersion` and `kind` fields of their deployment and add the strategy to the user wants to leverage.  Below is an example of a rollout resource that leverages a `BlueGreenUpdate` strategy with comments on which fields that were changed/added to convert a deployment into a rollout.

```yaml
apiVersion: argoproj.io/v1alpha1 # Changed from apps/v1
kind: Rollout # Changed from Deployment
# ----- Everything between these comments are the same as a deployment -----
metadata:
  name: example-rollout
spec:
  replicas: 1
  selector:
    matchLabels:
      app: nginx
  template:
    metadata:
      labels:
        app: nginx
    spec:
      containers:
      - name: nginx
        image: nginx:1.15.4
        ports:
        - containerPort: 80
  minReadySeconds: 30
  revisionHistoryLimit: 3
  strategy:
  # ----- Everything between these comments are the same as a deployment -----
    type: BlueGreenUpdate # Changed from RollingUpdate
    blueGreen: # A new field that used to provide configurable options for a BlueGreenUpdate strategy
      previewService: my-service-preview # Reference to a service that can serve traffic to a new image before it receives the active traffic  
      activeService: my-service-active # Reference to a service that serves end-user traffic to the replica set
```

## Deployment Strategies
While the industry has agreed upon high-level definitions of various deployment strategies, the implementations of these strategies tend to differ across tooling.  To make it clear how the rollout-controller will behave, here are the descriptions of the various deployment strategies implementations offered by the rollout-controller.

### Rolling Update
The `RollingUpdate` is still in development, but the intent is have the same behavior as the deployment's `RollingUpdate` strategy.

### Blue Green Update
In addition to managing replicasets, the rollout-controller will modify service resources during the `BlueGreenUpdate` strategy.  In the rollout spec, users will specify an active service and optionally a preview service. The active and preview service are references to existing services in the same namespace as the rollout.  The rollout-controller will modify the services' selectors to add a label that points them at the replicasets created by the rollout controller.  This allows the rollout to define an active and preview stack and a process to migrate replicasets from the preview to the active.  To achieve this process, the rollout controller constantly runs the following reconciliation:

1. Reconcile if the rollout has created a replicaset from its pod spec, and the new replicaset is fully available (all the pods are ready).
1. Reconcile if the preview service is serving traffic to the new replicaset.
    1. Skip this step if 
        * The preview service is not defined in the rollout spec.
        * The active service is not serving any traffic to any replicasets created by this rollout. 
        * The active service's selector already points at the new replicaset. 
    1. Verify if the preview service is serving traffic to the new replicaset.
        * Otherwise, set the preview service's selector to the new replicaset and set the verifyingPreview flag in the rollout status to true.
1. Check if the verifyingPreview flag is set to true.
    * Do not progress until verifyingPreview is unset or set to false.
1. Reconcile if the active service is serving traffic to the new replicaset
    1. Verify if the active service is serving traffic to the new replicaset.
        * Set the active service's selector to the new replica set
1. Scale down the old replica set that previously received traffic from the active service.

The rollout-controller will continuously run through these steps for any rollout resources.


### Canary
A Canary strategy is still in development and will likely leverage a service mesh tool like Istio in order to control fine grain traffic percentages.

## Installation

Two sets of installation manifests are provided:

* manfiest/install.yaml - Standard rollout-controller installation with cluster access. Use this manifest set if you plan to use the rollout-controller to deploy applications through the entire cluster.

* manfiest/namespace-install.yaml - Installation of rollout-controller which requires only namespace level privileges (does not need cluster roles). Use this manifest set if you want to only manage rollouts in a specific namespace.

You can install the rollout-controller using either of these manifests by using kubectl apply or leveraging a GitOps tool like [argo-cd](https://github.com/argoproj/argo-cd) to deploy the rollout-controller
