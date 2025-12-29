 # StatefulSet Plugin

This document describes the StatefulSet plugin implementation for RolloutPlugin controller.

## Overview

The StatefulSet plugin enables progressive canary deployments for Kubernetes StatefulSets by leveraging the native `partition` field in the StatefulSet's rolling update strategy.

## How It Works

### Canary Deployment Strategy

The plugin uses the StatefulSet `partition` field to control which pods get updated during a rollout:

1. **Weight-based Updates**: The `setWeight` value (0-100) represents the percentage of pods to update
2. **Partition Calculation**: `partition = replicas - (replicas * weight / 100)`
3. **Progressive Rollout**: Pods with ordinal >= partition remain at the old version

### Example

For a StatefulSet with 10 replicas:

| Weight | Partition | Pods Updated | Description |
|--------|-----------|--------------|-------------|
| 0%     | 10        | 0 pods       | No pods updated |
| 20%    | 8         | pods 8-9     | 2 pods updated (20%) |
| 40%    | 6         | pods 6-9     | 4 pods updated (40%) |
| 60%    | 4         | pods 4-9     | 6 pods updated (60%) |
| 80%    | 2         | pods 2-9     | 8 pods updated (80%) |
| 100%   | 0         | pods 0-9     | All pods updated |

**Note**: StatefulSets update pods in descending order (N-1, N-2, ..., 0).

## Plugin Interface

The StatefulSet plugin implements the `ResourcePlugin` interface with the following methods:

### Init()
Initializes the plugin. Called once when the plugin is first loaded.

### GetResourceStatus(ctx, workloadRef)
Retrieves the current status of the StatefulSet including:
- Replica counts (total, updated, ready, available)
- Current and updated revisions
- Ready status

### SetWeight(ctx, workloadRef, weight)
Updates the StatefulSet partition field to achieve the desired weight:
```go
partition = replicas - (replicas * weight / 100)
```

### VerifyWeight(ctx, workloadRef, weight)
Verifies that:
1. Partition field is set correctly
2. Expected number of pods are updated
3. All pods are ready
4. StatefulSet has observed the latest generation

### Promote(ctx, workloadRef)
Completes the rollout by setting `partition: 0` (all pods updated).

### Abort(ctx, workloadRef)
Aborts the rollout by setting `partition: replicas` (reverts all pods to previous version).

## Usage

### 1. Create a StatefulSet

```yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: my-statefulset
  namespace: argo-rollouts
spec:
  serviceName: my-statefulset
  replicas: 5
  selector:
    matchLabels:
      app: my-app
  template:
    metadata:
      labels:
        app: my-app
    spec:
      containers:
      - name: nginx
        image: nginx:1.21
        ports:
        - containerPort: 80
  updateStrategy:
    type: RollingUpdate
    rollingUpdate:
      partition: 5  # Start with no pods updated
```

### 2. Create a RolloutPlugin

```yaml
apiVersion: argoproj.io/v1alpha1
kind: RolloutPlugin
metadata:
  name: my-statefulset-rollout
  namespace: argo-rollouts
spec:
  workloadRef:
    apiVersion: apps/v1
    kind: StatefulSet
    name: my-statefulset
    namespace: argo-rollouts
  
  plugin:
    name: statefulset-plugin
  
  strategy:
    canary:
      steps:
        - setWeight: 20   # Update 1 pod (20% of 5)
        - pause: {duration: 5m}
        - setWeight: 40   # Update 2 pods (40% of 5)
        - pause: {duration: 5m}
        - setWeight: 60   # Update 3 pods (60% of 5)
        - pause: {duration: 5m}
        - setWeight: 80   # Update 4 pods (80% of 5)
        - pause: {duration: 5m}
        - setWeight: 100  # Update all 5 pods
```

### 3. Trigger a Rollout

Update the StatefulSet to trigger a rollout:

```bash
kubectl set image statefulset/my-statefulset nginx=nginx:1.22 -n argo-rollouts
```

The RolloutPlugin controller will automatically:
1. Detect the change (different updateRevision)
2. Progress through each canary step
3. Update the partition field at each step
4. Wait for pods to be ready before continuing
5. Complete the rollout when all steps are done

## Monitoring

### Check RolloutPlugin Status

```bash
kubectl get rolloutplugin my-statefulset-rollout -n argo-rollouts -o yaml
```

Key status fields:
- `status.phase`: Current phase (Progressing, Paused, Successful, Failed)
- `status.currentStepIndex`: Current step in the canary strategy
- `status.message`: Human-readable status message
- `status.replicas`: Total replicas
- `status.updatedReplicas`: Number of pods at the new version
- `status.readyReplicas`: Number of ready pods

### Check StatefulSet Status

```bash
kubectl get statefulset my-statefulset -n argo-rollouts
```

Key fields:
- `spec.updateStrategy.rollingUpdate.partition`: Current partition value
- `status.updatedReplicas`: Number of pods at the new version
- `status.currentRevision`: Previous revision
- `status.updateRevision`: New revision

### Check Pod Status

```bash
kubectl get pods -n argo-rollouts -l app=my-app
```

Observe which pods are at the new version:
- Pods with ordinal < partition: Old version
- Pods with ordinal >= partition: New version (or updating)

## Testing

Use the provided test script to verify the StatefulSet plugin functionality:

```bash
./test/rolloutplugin/test-statefulset-canary.sh
```

This script will:
1. Create a test StatefulSet with 5 replicas
2. Update the StatefulSet image
3. Create a RolloutPlugin with canary strategy
4. Monitor the rollout progression
5. Verify each step completes successfully
6. Clean up resources

## Implementation Details

### Architecture

The StatefulSet plugin is implemented as a **built-in plugin** (similar to metric providers in Argo Rollouts):

- **Built-in Mode**: Runs in-process with the controller
- **No RPC Overhead**: Direct function calls for better performance
- **Native Kubernetes Kind**: StatefulSet is a core Kubernetes resource

This follows the same pattern as metric providers (Prometheus, Datadog, etc.) which are also built-in.

### File Structure

```
rolloutplugin/plugins/statefulset/
└── plugin.go          # Built-in StatefulSet plugin implementation
```

### Dependencies

- `k8s.io/client-go/kubernetes`: Kubernetes client for StatefulSet operations
- `github.com/sirupsen/logrus`: Logging
- `github.com/argoproj/argo-rollouts/rolloutplugin`: Plugin interface

### Registration

The plugin is registered as a built-in plugin in `cmd/rolloutplugin-controller/main.go`:

```go
// Register built-in plugins (similar to metric providers pattern)
logrusCtx := log.WithField("plugin", "statefulset")
statefulSetPlugin := statefulset.NewPlugin(kubeClientset, logrusCtx)
wrappedPlugin := pluginPackage.NewRolloutPlugin(statefulSetPlugin)
pluginManager.RegisterPlugin("statefulset", wrappedPlugin)
```

### RPC Plugin Support

While StatefulSet is built-in, the RPC plugin infrastructure remains available for third-party plugins that manage custom or external workload types. This allows extending RolloutPlugin to support non-Kubernetes resources or proprietary workload types.

## Limitations

1. **No Traffic Routing**: This plugin only controls pod updates, not traffic distribution
2. **Descending Order**: StatefulSets update pods in descending order (N-1, N-2, ..., 0)
3. **State Management**: Applications must handle state synchronization between old and new versions
4. **No Automated Rollback**: Abort requires manual intervention or external monitoring

## Future Enhancements

1. **Analysis Support**: Integration with Argo Rollouts Analysis for automated health checks
2. **Custom Health Checks**: Plugin-specific readiness verification
3. **External Plugin Support**: Load plugins from external URLs using HashiCorp go-plugin
4. **Multi-Stage Rollouts**: Support for more complex rollout patterns

## References

- [Proposal Document](../../docs/proposals/resource-plugin.md)
- [Kubernetes StatefulSet Documentation](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/)
- [StatefulSet Update Strategies](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/#update-strategies)
