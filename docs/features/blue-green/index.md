# BlueGreen Deployment Strategy
A Blue Green Deployment allows users to reduce the amount of time multiple versions running at the same time.

## Overview

In addition to managing ReplicaSets, the rollout controller will modify a Service resource during the `BlueGreenUpdate` strategy.  The Rollout spec has users specify a reference to active service and optionally a preview service in the same namespace. The active Service is used to send regular application traffic to the old version, while the preview Service is used as funnel traffic to the new version. The rollout controller ensures proper traffic routing by injecting a unique hash of the ReplicaSet to these services' selectors.  This allows the rollout to define an active and preview stack and a process to migrate replica sets from the preview to the active. 

When there is a change to the `.spec.template` field of a rollout, the controller will create the new ReplicaSet.  If the active service is not sending traffic to a ReplicaSet, the controller will immediately start sending traffic to the ReplicaSet. Otherwise, the active service will point at the old ReplicaSet while the ReplicaSet becomes available. Once the new ReplicaSet becomes available, the controller will modify the active service to point at the new ReplicaSet. After waiting some time configured by the `.spec.strategy.blueGreen.scaleDownDelaySeconds`, the controller will scale down the old ReplicaSet.

__Important note__
When the rollout changes the selector on a service, there is a propagation delay before all the nodes update their IP tables to send traffic to the new pods instead of the old. During this delay, traffic will be directed to the old pods if the nodes have not been updated yet. In order to prevent the packets from being sent to a node that killed the old pod, the rollout uses the scaleDownDelaySeconds field to give nodes enough time to broadcast the IP table changes.

## Configurable Features
Here are the optional fields that will change the behavior of BlueGreen deployment:
```yaml
spec:
  strategy:
    blueGreen:
      previewService: string
      previewReplicaCount: *int32
      autoPromotionEnabled: true
      autoPromotionSeconds: *int32
      scaleDownDelaySeconds: *int32
      scaleDownDelayRevisionLimit: *int32
      antiAffinityBetweenVersion: bool
```

### PreviewService
The PreviewService field references a Service that will be modified to send traffic to the new replicaset before the new one is promoted to receiving traffic from the active service. Once the new replicaset start receives traffic from the active service, the preview service will be modified to send traffic to no ReplicaSets. The Rollout always makes sure that the preview service is sending traffic to the new ReplicaSet.  As a result, if a new version is introduced before the old version is promoted to the active service, the controller will immediately switch over to the new version.

This feature is used to provide an endpoint that can be used to test a new version of an application.

Defaults to an empty string

### PreviewReplicaCount
The PreviewReplicaCount will indicate the number of replicas that the new version of an application should run.  Once the application is ready to promote to the active service, the controller will scale the new ReplicaSet to the value of the `spec.replicas`. The rollout will not switch over the active service to the new ReplicaSet until it matches the `spec.replicas` count.

This feature is mainly used to save resources during the testing phase. If the application does not need a fully scaled up application for the tests, this feature can help save some resources.

Defaults to nil

### AutoPromotionEnabled
The AutoPromotionEnabled will make the rollout automatically promote the new ReplicaSet to the active service once the new ReplicaSet is healthy. This field is defaulted to true if it is not specified.

Defaults to true

### AutoPromotionSeconds
The AutoPromotionSeconds will make the rollout automatically promote the new ReplicaSet to active Service after the AutoPromotionSeconds time has passed since the rollout has entered a paused state. If the `AutoPromotionEnabled` field is set to true, this field will be ignored

Defaults to nil


### ScaleDownDelaySeconds
The ScaleDownDelaySeconds is used to delay scaling down the old ReplicaSet after the active Service is switched to the new ReplicaSet.

Defaults to 30

### ScaleDownDelayRevisionLimit
The ScaleDownDelayRevisionLimit limits the number of old active ReplicaSets to keep scaled up while they wait for the scaleDownDelay to pass after being removed from the active service. 

Default to nil


### AntiAffinityBetweenVersion

Check out the [Blue Green Anti Affinity document](anti-affinity/anti-affinity.md) document for more information
