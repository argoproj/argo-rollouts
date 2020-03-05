# Canary Deployment Strategy
A canary rollout is a deployment strategy where the operator releases a new version of their application to a small percentage of the production traffic. 

## Overview
Since there is no agreed upon standard for a canary deployment, the rollouts controller allows users to outline how they want to run their canary deployment. Users can define a list of steps the controller uses to manipulate the RepliaSets where there is a change to the `.spec.template`. Each step will be evaluated before the new ReplicaSet is promoted to the stable version, and the old version is completely scaled down.

Each step can have one of two fields. The `setWeight` field dictates the percentage of traffic that should be sent to the canary, and the `pause` struct instructs the rollout to pause.  When the controller reaches a `pause` step for a rollout, it will set the `.spec.paused` field to `true`. If the `duration` field within the `pause` struct is set, the rollout will not progress to the next step until it has waited for the value of the `duration` field. Otherwise, the rollout will wait indefinitely until the `.spec.paused` field is set to `false`. By using the `setWeight` and the `pause` fields, a user can declarative describe how they want to progress to the new version. Below is an example of a canary strategy.

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
          duration: "1h" # 1 hour
      - setWeight: 20
      - pause: {} # pause indefinitely
```

### Pause Duration
Pause duration can be specied with an optional time unit suffix. Valid time units are "s", "m", "h". Defaults to "s" if not specified. Values less than zero are not allowed. 

```yaml
spec:
  strategy:
    canary:
      steps:
        - pause: { duration: 10 }  # 10 seconds
        - pause: { duration: 10s } # 10 seconds
        - pause: { duration: 10m } # 10 minutes
        - pause: { duration: 10h } # 10 hours
        - pause: { duration: -10 } # invalid spec!
        - pause: {}                # pause indefinitely
```

If no `duration` specified for a pause step the rollout will be paused indefinitely. To unpause use the [argo kubectl plugin](kubectl-plugin.md) `promote` command. 

```shell
# promote to the next step
kubectl argo rollouts promote <rollout>
```

## Mimicking Rolling Update
If the steps field is omitted, the canary strategy will mimic the rolling update behavior. Similar to the deployment, the canary strategy has the `maxSurge` and `maxUnavailable` fields to configure how the Rollout should progress to the new version.

## Other Configurable Features
Here are the optional fields that will modify the behavior of canary strategy:
```yaml
spec:
  strategy:
    canary:
      maxSurge: stringOrInt
      maxUnavailable: stringOrInt
      canaryService: string
```

### maxSurge
`maxSurge` defines the maximum number of replicas the rollout can create to move to the correct ratio set by the last setWeight. Max Surge can either be an integer or percentage as a string (i.e. "20%")

Defaults to "25%".

### maxUnavailable
The maximum number of pods that can be unavailable during the update. Value can be an absolute number (ex: 5) or a percentage of desired pods (ex: 10%). This can not be 0 if MaxSurge is 0.

Defaults to 0

### canaryService
`canaryService` references a Service that will be modified to send traffic to only the canary ReplicaSet. This allows users to only hit the canary ReplicaSet.

Defaults to an empty string
