
# Argo Rollouts - Advanced Kubernetes Deployment Controller
[![codecov](https://codecov.io/gh/argoproj/argo-rollouts/branch/master/graph/badge.svg)](https://codecov.io/gh/argoproj/argo-rollouts)
[![slack](https://img.shields.io/badge/slack-argoproj-brightgreen.svg?logo=slack)](https://argoproj.github.io/community/join-slack)

## What is Argo Rollouts?
Argo Rollouts controller, uses the Rollout custom resource to provide additional deployment strategies such as Blue Green and Canary to Kubernetes.  The Rollout custom resource provides feature parity with the deployment resource but with additional deployment strategies.

## Why use Argo Rollouts?
Deployments resources offer two strategies to deploy changes: `RollingUpdate` and `Recreate`. While these strategies can solve a wide number of use cases, large scale production deployments use additional strategies, such as blue-green or canary, that are missing from the Deployment controller.  In order to use these strategies in Kubernetes, users are forced to build scripts on top of their deployments. The Argo Rollouts controller provides these strategies as simple declarative, configurable options.

## Use cases of Argo Rollouts

- A user wants to run last minute functional tests on the new version before it starts to serve production traffic.  With the BlueGreen strategy, Argo Rollouts allow users to specify a preview service and an active service. The Rollout will configure the preview service to send traffic to the new version while the active service continues to receive production traffic. Once a user is satisfied, they can promote the preview service to be the new active service. ([example](examples/example-rollout-bluegreen.yaml))

- Before a new version starts receiving live traffic, a generic set of steps need to be executed beforehand. With the BlueGreen Strategy, the user can bring up the new version without it receiving traffic from the active service. Once those steps finish executing, the rollout can cut over traffic to the new version.

- A user wants to give a small percentage of the production traffic to a new version of their application for a couple of hours.  Afterwards, they want to scale down the new version and look at some metrics to determine if the new version is performant compared to the old version. Then they will decide if they want to rollout the new version for all of the production traffic or stick with the current version. With the canary strategy, the rollout can scale up a replica with the new version to receive a specified percentage of traffic, wait for a specified amount of time, set the percentage back to 0, and then wait to rollout out to service all of the traffic once the user is satisfied. ([example](examples/example-rollout-canary-run-tmp-canary.yaml))

- A user wants to slowly give the new version more production traffic. They start by giving it a small percentage of the live traffic and wait a while before giving the new version more traffic. Eventually, the new version will receive all the production traffic. With the canary strategy, the user specifies the percentages they want the new version to receive and the amount of time to wait between percentages. ([example](examples/example-rollout-canary.yaml))

- A user wants to use the normal Rolling Update strategy from the deployment. If a user uses the canary strategy with no steps, the rollout will use the max surge and max unavailable values to roll to the new version. ([example](examples/example-rollout-canary-rolling-update.yaml))

## Spec
One of the design considerations of the Rollout resource is making the transition from a deployment to a rollout painless.  In service to that goal, the rollout spec has the same fields as a deployment spec.  However, the strategy field in the rollout spec has additional options available like `BlueGreenUpdate` or `Canary`.  As a result, a user who wants to move from a deployment will change the `apiVersion` and `kind` fields of their deployment and add the strategy to the user wants to leverage.  Below is an example of a rollout resource that leverages a `BlueGreenUpdate` strategy with comments on which fields that were changed/added to convert a deployment into a rollout.

```yaml
apiVersion: argoproj.io/v1alpha1 # Changed from apps/v1
kind: Rollout # Changed from Deployment
# ----- Everything below this comment is the same as a deployment -----
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
  # ----- Everything above this comment are the same as a deployment -----
    blueGreen: # A new field that used to provide configurable options for a BlueGreenUpdate strategy
      previewService: my-service-preview # Reference to a service that can serve traffic to a new image before it receives the active traffic  
      activeService: my-service-active # Reference to a service that serves end-user traffic to the replica set
```

## Deployment Strategies
While the industry has agreed upon high-level definitions of various deployment strategies, the implementations of these strategies tend to differ across tooling.  To make it clear how the Argo Rollouts will behave, here are the descriptions of the various deployment strategies implementations offered by the Argo Rollouts.

### Blue Green Update
In addition to managing replica sets, the Argo Rollouts will modify `Service` resources during the `BlueGreenUpdate` strategy.  In the rollout spec, users will specify an active service and optionally a preview service. The active and preview service are references to existing services in the same namespace as the rollout.  The Argo Rollouts will modify the services' selectors to add a label that points them at the replica sets created by the rollout controller.  This allows the rollout to define an active and preview stack and a process to migrate replica sets from the preview to the active.  To achieve this process, the rollout controller constantly runs the following reconciliation:

1. Reconcile if the rollout has created a replica set from its pod spec, and the new replica set is fully available (all the pods are ready).
1. Reconcile if the preview service is serving traffic to the new replica set.
    1. Skip this step if 
        * The preview service is not defined in the rollout spec.
        * The active service is not serving any traffic to any replica sets created by this rollout. 
        * The active service's selector already points at the new replica set. 
    1. Verify if the preview service is serving traffic to the new replica set.
        * Otherwise, set the preview service's selector to the new replica set and set the `paused` flag and `pausedStartedAt` field in the rollout status to true.
1. Check if the `paused` flag is set to true.
    * Do not progress until `paused` is set to false.
1. Reconcile if the active service is serving traffic to the new replica set
    1. Verify if the active service is serving traffic to the new replica set.
        * Set the active service's selector to the new replica set
1. Scale down the old replica set that previously received traffic from the active service.

The Argo Rollouts will continuously run through these steps for any rollout resources.


### Canary
A canary rollout is a deployment strategy where the operator releases a new version of their application to a small percentage of the production traffic. Canaries are often used to verify the new release is functional and performant without having the risk of exposing the new version to all traffic.  While the Kubernetes community generally agrees with the principles of a canary, their implementations of canary tend to vastly differ.  Most of these differences tend to come from the analysis that indicates if the canary should be promoted. Argo Rollout sees that as a separate problem to be solved and focuses on providing the mechanics to run two versions at once with both receiving a specified amount of traffic in a declarative way. Argo Rollout's approach allows users to write a single GitOps friendly manifest that specifies a list of actions to follow when a change is submitted.  These steps will dictate how much traffic should go to the canary version and how long the controller should remain at a step.  Below is an example of a rollout with a canary strategy:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: example-rollout
spec:
  replicas: 10
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
    canary: #Indicates that the rollout should use the Canary strategy
      maxSurge: "25%"
      maxUnavailable: 0,
      steps:
      - setWeight: 10
      - pause:
          duration: 3600 # 1 hour
      - setWeight: 20
      - pause: {}
```
The `steps` struct in the example above defines how the controller should behave when a new version is submitted. Each step will be evaluated before the new replica set is promoted to the stable version, and the old version is completely scaled down. The `setWeight` field dictates the percentage of traffic that should be sent to the canary, and the `pause` struct instructs the rollout to pause.  When the controller reaches a `pause` step for a rollout, it will set the `.spec.paused` field to `true`. If the `duration` field within the `pause` struct is set, the rollout will not progress to the next step until it has waited for the value of the `duration` field. Otherwise, the rollout will wait indefinitely until the `.spec.paused` field is set to `false`. By using the `setWeight` and the `pause` fields, a user can declarative describe how they want to progress to the new version.

#### Traffic Routing
When `setWeight` sets the percentage that goes to the new version of the application, Argo Rollouts will provide multiple ways to shape the traffic.  Argo Rollouts supports replicaset-based traffic shaping initially, and it will add support for service mesh technologies in the future. The initial Replicaset-based traffic shaping scales the new and stable replica sets to the `setWeight` percentages of the replica count and relies on the round-robin behavior of a Kubernetes service to route traffic to these replica sets evenly. As an example, if a rollout had 10 replicas and a `setWeight` of 20, it would scale up 2 replicas of the new replica set, and scale down the stable replica set to 8. The `maxSurge` and `maxUnavailable` fields within the `canary` structure determine how the rollout will scale the replicas to reach the desired `setWeight`.  In the case of a `setWeight` and replica count that does not divide into whole numbers (i.e. 5% for 10 replicas), the Rollout will round up the result for both the new and stable replica sets (i.e. 1 replicas for the new replica set and 9 replicas for the stable replica set) in order to prevent the either replica set from having 0 replicas.  This will hold for any percentage except for 0% and 100% where the rollout will completely scale up new or stable replica set.

In order to offer more fine-grain traffic shaping, Argo Rollout will provide integrations with other tools like Istio and AWS Service Mesh in the future. As a result, the `setWeight` will honor the percentage instead of making a best effort with some additional configuration. These solutions are currently being discussed and will be implemented in a short time.

### Rolling Update
The Rolling update strategy can be achieved by using the `Canary` strategy with no steps.  The Rollout will use the `maxSurge` and `maxUnavailable` as upper and lower bounds to guide the old version to the new one.

## Installation

Two sets of installation manifests are provided:

* manifest/install.yaml - Standard Argo Rollouts installation with cluster access. Use this manifest set if you plan to use the Argo Rollouts to deploy applications through the entire cluster.

* manifest/namespace-install.yaml - Installation of Argo Rollouts which requires only namespace level privileges (does not need cluster roles). Use this manifest set if you want to only manage rollouts in a specific namespace.

You can install the Argo Rollouts using either of these manifests by using running the kubectl apply with either file or leveraging a GitOps tool like [argo-cd](https://github.com/argoproj/argo-cd) to deploy the Argo Rollouts.  Below is an example of how to install Argo Rollouts at a cluster-wide level.
```bash
$ kubectl create namespace argo-rollouts
$ kubectl apply -n argo-rollouts -f https://raw.githubusercontent.com/argoproj/argo-rollouts/stable/manifests/install.yaml
```
