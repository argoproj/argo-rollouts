# BlueGreen Deployment Strategy

A Blue Green Deployment allows users to reduce the amount of time multiple versions running at the same time.

## Overview

In addition to managing ReplicaSets, the rollout controller will modify a Service resource during the `BlueGreenUpdate` strategy.  The Rollout spec has users specify a reference to active service and optionally a preview service in the same namespace. The active Service is used to send regular application traffic to the old version, while the preview Service is used as funnel traffic to the new version. The rollout controller ensures proper traffic routing by injecting a unique hash of the ReplicaSet to these services' selectors.  This allows the rollout to define an active and preview stack and a process to migrate replica sets from the preview to the active. 

When there is a change to the `.spec.template` field of a rollout, the controller will create the new ReplicaSet.  If the active service is not sending traffic to a ReplicaSet, the controller will immediately start sending traffic to the ReplicaSet. Otherwise, the active service will point at the old ReplicaSet while the ReplicaSet becomes available. Once the new ReplicaSet becomes available, the controller will modify the active service to point at the new ReplicaSet. After waiting some time configured by the `.spec.strategy.blueGreen.scaleDownDelaySeconds`, the controller will scale down the old ReplicaSet.

!!! important
    When the rollout changes the selector on a service, there is a propagation delay before all the nodes update their IP tables to send traffic to the new pods instead of the old. During this delay, traffic will be directed to the old pods if the nodes have not been updated yet. In order to prevent the packets from being sent to a node that killed the old pod, the rollout uses the scaleDownDelaySeconds field to give nodes enough time to broadcast the IP table changes.

## Example

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: rollout-bluegreen
spec:
  replicas: 2
  revisionHistoryLimit: 2
  selector:
    matchLabels:
      app: rollout-bluegreen
  template:
    metadata:
      labels:
        app: rollout-bluegreen
    spec:
      containers:
      - name: rollouts-demo
        image: argoproj/rollouts-demo:blue
        imagePullPolicy: Always
        ports:
        - containerPort: 8080
  strategy:
    blueGreen: 
      # activeService specifies the service to update with the new template hash at time of promotion.
      # This field is mandatory for the blueGreen update strategy.
      activeService: rollout-bluegreen-active
      # previewService specifies the service to update with the new template hash before promotion.
      # This allows the preview stack to be reachable without serving production traffic.
      # This field is optional.
      previewService: rollout-bluegreen-preview
      # autoPromotionEnabled disables automated promotion of the new stack by pausing the rollout
      # immediately before the promotion. If omitted, the default behavior is to promote the new
      # stack as soon as the ReplicaSet are completely ready/available.
      # Rollouts can be resumed using: `kubectl argo rollouts promote ROLLOUT`
      autoPromotionEnabled: false
```

## Configurable Features
Here are the optional fields that will change the behavior of BlueGreen deployment:
```yaml
spec:
  strategy:
    blueGreen:
      autoPromotionEnabled: boolean
      autoPromotionSeconds: *int32
      antiAffinity: object
      previewService: string
      prePromotionAnalysis: object
      postPromotionAnalysis: object
      previewReplicaCount: *int32
      scaleDownDelaySeconds: *int32
      scaleDownDelayRevisionLimit: *int32
```

### autoPromotionEnabled
The AutoPromotionEnabled will make the rollout automatically promote the new ReplicaSet to the active service once the new ReplicaSet is healthy. This field is defaulted to true if it is not specified.

Defaults to true

### autoPromotionSeconds
The AutoPromotionSeconds will make the rollout automatically promote the new ReplicaSet to active Service after the AutoPromotionSeconds time has passed since the rollout has entered a paused state. If the `AutoPromotionEnabled` field is set to true, this field will be ignored

Defaults to nil

### antiAffinity
Check out the [Anti Affinity document](anti-affinity/anti-affinity.md) document for more information.

Defaults to nil

### maxUnavailable
The maximum number of pods that can be unavailable during the update. Value can be an absolute number (ex: 5) or a percentage of desired pods (ex: 10%). This can not be 0 if MaxSurge is 0.

Defaults to 0

### prePromotionAnalysis
Configures the [Analysis](analysis.md#bluegreen-pre-promotion-analysis) before it switches traffic to the new version. The
AnalysisRun can be used to block the Service selector switch until the AnalysisRun finishes successful. The success or
failure of the analysis run decides if the Rollout will switch traffic, or abort the Rollout completely.

Defaults to nil

### postPromotionAnalysis
Configures the [Analysis](analysis.md#bluegreen-pre-promotion-analysis) after the traffic switch to new version. If the analysis
run fails or errors out, the Rollout enters an aborted state and switch traffic back to the previous stable Replicaset.
If `scaleDownDelaySeconds` is specified, the controller will cancel any AnalysisRuns at time of `scaleDownDelay` to 
scale down the ReplicaSet. If it is omitted, and post analysis is specified, it will scale down the ReplicaSet only 
after the AnalysisRun completes (with a minimum of 30 seconds).

Defaults to nil

### previewService
The PreviewService field references a Service that will be modified to send traffic to the new ReplicaSet before the new one is promoted to receiving traffic from the active service. Once the new ReplicaSet starts receiving traffic from the active service, the preview service will also be modified to send traffic to the new ReplicaSet as well. The Rollout always makes sure that the preview service is sending traffic to the newest ReplicaSet.  As a result, if a new version is introduced before the old version is promoted to the active service, the controller will immediately switch over to that brand new version.

This feature is used to provide an endpoint that can be used to test a new version of an application.

Defaults to an empty string

Here is a timeline of how the active and preview services work (if you use a preview service):

1. During the Initial deployment there is only one ReplicaSet. Both active and preview services point to it. This is the **old** version of the application.
1. A change happens in the Rollout resource. A new ReplicaSet is created. This is the **new** version of the application. The preview service is modified to point to the new ReplicaSet. The active service still points to the old version.
1. The blue/green deployment is "promoted". Both active and preview services are pointing to the new version. The old version is still there but no service is pointing at it.
1. Once the the blue/green deployment is scaled down (see the `scaleDownDelaySeconds` field) the old ReplicaSet is has 0 replicas and we are back to the initial state. Both active and preview services point to the new version (which is the only one present anyway)



### previewReplicaCount
The PreviewReplicaCount field will indicate the number of replicas that the new version of an application should run.  Once the application is ready to promote to the active service, the controller will scale the new ReplicaSet to the value of the `spec.replicas`. The rollout will not switch over the active service to the new ReplicaSet until it matches the `spec.replicas` count.

This feature is mainly used to save resources during the testing phase. If the application does not need a fully scaled up application for the tests, this feature can help save some resources.

If omitted, the preview ReplicaSet stack will be scaled to 100% of the replicas.

### scaleDownDelaySeconds
The ScaleDownDelaySeconds is used to delay scaling down the old ReplicaSet after the active Service is switched to the new ReplicaSet.

Defaults to 30

### scaleDownDelayRevisionLimit
The ScaleDownDelayRevisionLimit limits the number of old active ReplicaSets to keep scaled up while they wait for the scaleDownDelay to pass after being removed from the active service. 

If omitted, all ReplicaSets will be retained for the specified scaleDownDelay
