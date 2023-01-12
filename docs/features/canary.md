# Canary Deployment Strategy
A canary rollout is a deployment strategy where the operator releases a new version of their application to a small percentage of the production traffic. 

## Overview
Since there is no agreed upon standard for a canary deployment, the rollouts controller allows users to outline how they want to run their canary deployment. Users can define a list of steps the controller uses to manipulate the ReplicaSets when there is a change to the `.spec.template`. Each step will be evaluated before the new ReplicaSet is promoted to the stable version, and the old version is completely scaled down.

Each step can have one of two fields. The `setWeight` field dictates the percentage of traffic that should be sent to the canary, and the `pause` struct instructs the rollout to pause.  When the controller reaches a `pause` step for a rollout, it will add a `PauseCondition` struct to the `.status.PauseConditions` field. If the `duration` field within the `pause` struct is set, the rollout will not progress to the next step until it has waited for the value of the `duration` field. Otherwise, the rollout will wait indefinitely until that Pause condition is removed. By using the `setWeight` and the `pause` fields, a user can declaratively describe how they want to progress to the new version. Below is an example of a canary strategy.

!!! important
    If the canary Rollout does not use [traffic management](traffic-management/index.md), the Rollout makes a best effort attempt to achieve the percentage listed in the last `setWeight` step between the new and old version. For example, if a Rollout has 10 Replicas and 10% for the first `setWeight` step, the controller will scale the new desired ReplicaSet to 1 replicas and the old stable ReplicaSet to 9. In the case where the setWeight is 15%, the Rollout attempts to get there by rounding up the calculation (i.e. the new ReplicaSet has 2 pods since 15% of 10 rounds up to 2 and the old ReplicaSet has 9 pods since 85% of 10 rounds up to 9). If a user wants to have more fine-grained control of the percentages without a large number of Replicas, that user should use the  [traffic management](#trafficrouting) functionality.

## Example
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
      maxUnavailable: 0
      steps:
      - setWeight: 10
      - pause:
          duration: 1h # 1 hour
      - setWeight: 20
      - pause: {} # pause indefinitely
```

## Pause Duration
Pause duration can be specified with an optional time unit suffix. Valid time units are "s", "m", "h". Defaults to "s" if not specified.

```yaml
spec:
  strategy:
    canary:
      steps:
        - pause: { duration: 10 }  # 10 seconds
        - pause: { duration: 10s } # 10 seconds
        - pause: { duration: 10m } # 10 minutes
        - pause: { duration: 10h } # 10 hours
        - pause: {}                # pause indefinitely
```

If no `duration` is specified for a pause step, the rollout will be paused indefinitely. To unpause, use the [argo kubectl plugin](kubectl-plugin.md) `promote` command. 

```shell
# promote to the next step
kubectl argo rollouts promote <rollout>
```

## Dynamic Canary Scale (with Traffic Routing)

By default, the rollout controller will scale the canary to match the current trafficWeight of the
current step. For example, if the current weight is 25%, and there are four replicas, then the
canary will be scaled to 1, to match the traffic weight.

It is possible to control the canary replica's scale during the steps such that it does not necessary
match the traffic weight. Some use cases for this:

1. The new version should not yet be exposed to the public (setWeight: 0), but you would like to
   scale the canary up for testing purposes.
2. You wish to scale the canary stack up minimally, and use some header based traffic shaping to
   the canary, while setWeight is still set to 0.
3. You wish to scale the canary up to 100%, in order to facilitate traffic shadowing.


!!! important
    Setting canary scale is only available when using the canary strategy with a traffic router, since the basic canary needs to control canary scale in order to approximate canary weight.

To control canary scales and weights during steps, use the `setCanaryScale` step and indicate which scale the
the canary should use:

* explicit replica count without changing traffic weight (`replicas`)
* explicit weight percentage of total spec.replicas without changing traffic weight(`weight`)
* to or not to match current canary's `setWeight` step (`matchTrafficWeight: true or false`)

```yaml
spec:
  strategy:
    canary:
      steps:
      # explicit count
      - setCanaryScale:
          replicas: 3
      # a percentage of spec.replicas
      - setCanaryScale:
          weight: 25
      # matchTrafficWeight returns to the default behavior of matching the canary traffic weight
      - setCanaryScale:
          matchTrafficWeight: true
```

When using `setCanaryScale` with explicit values for either replicas or weight, one must be careful
if used in conjunction with the `setWeight` step. If done incorrectly, an imbalanced amount of traffic
may be directed to the canary (in proportion to the Rollout's scale). For example, the following set
of steps would cause 90% of traffic to only be served by 10% of pods:

```yaml
spec:
  replicas: 10
  strategy:
    canary:
      steps:
      # 1 canary pod (10% of spec.replicas)
      - setCanaryScale:
          weight: 10
      # 90% of traffic to the 1 canary pod
      - setWeight: 90
      - pause: {}
```

The above situation is caused by the changed behvaior of `setWeight` after `setCanaryScale`. To reset, set `matchTrafficWeight: true` and the `setWeight` behavior will be restored, i.e., subsequent `setWeight` will create canary replicas matching the traffic weight.

## Dynamic Stable Scale (with Traffic Routing)

!!! important
    Available since v1.1

When using traffic routing, by default the stable ReplicaSet is left scaled to 100% during the update.
This has the advantage that if an abort occurs, traffic can be immediately shifted back to the
stable ReplicaSet without delay. However, it has the disadvantage that during the update, there will
eventually exist double the number of replica pods running (similar to in a blue-green deployment),
since the stable ReplicaSet is left scaled up for the full duration of the update.

It is possible to dynamically reduce the scale of the stable ReplicaSet during an update such that
it scales down as the traffic weight increases to canary. This would be desirable in scenarios where
the Rollout has a high replica count and resource cost is a concern, or in bare-metal situations
where it is not possible to create additional node capacity to accommodate double the replicas.

The ability to dynamically scale the stable ReplicaSet can be enabled by setting the
`canary.dynamicStableScale` flag to true:

```yaml
spec:
  strategy:
    canary:
      dynamicStableScale: true
```

NOTE: that if `dynamicStableScale` is set, and the rollout is aborted, the canary ReplicaSet will
dynamically scale down as traffic shifts back to stable. If you wish to leave the canary ReplicaSet
scaled up while aborting, an explicit value for `abortScaleDownDelaySeconds` can be set:

```yaml
spec:
  strategy:
    canary:
      dynamicStableScale: true
      abortScaleDownDelaySeconds: 600
```


## Mimicking Rolling Update
If the `steps` field is omitted, the canary strategy will mimic the rolling update behavior. Similar to the deployment, the canary strategy has the `maxSurge` and `maxUnavailable` fields to configure how the Rollout should progress to the new version.

## Other Configurable Features
Here are the optional fields that will modify the behavior of canary strategy:

```yaml
spec:
  strategy:
    canary:
      analysis: object
      antiAffinity: object
      canaryService: string
      stableService: string
      maxSurge: stringOrInt
      maxUnavailable: stringOrInt
      trafficRouting: object
```

### analysis
Configure the background [Analysis](analysis.md) to execute during the rollout. If the analysis is unsuccessful the rollout will be aborted.

Defaults to nil

### antiAffinity
Check out the [Anti Affinity document](anti-affinity/anti-affinity.md) document for more information.

Defaults to nil

### canaryService
`canaryService` references a Service that will be modified to send traffic to only the canary ReplicaSet. This allows users to only hit the canary ReplicaSet.

Defaults to an empty string

### stableService
`stableService` the name of a Service which selects pods with stable version and doesn't select any pods with canary version. This allows users to only hit the stable ReplicaSet.

Defaults to an empty string

### maxSurge
`maxSurge` defines the maximum number of replicas the rollout can create to move to the correct ratio set by the last setWeight. Max Surge can either be an integer or percentage as a string (i.e. "20%")

Defaults to "25%".

### maxUnavailable
The maximum number of pods that can be unavailable during the update. Value can be an absolute number (ex: 5) or a percentage of desired pods (ex: 10%). This can not be 0 if MaxSurge is 0.

Defaults to 25%

### trafficRouting
The [traffic management](traffic-management/index.md) rules to apply to control the flow of traffic between the active and canary versions. If not set, the default weighted pod replica based routing will be used.

Defaults to nil
