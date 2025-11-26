---
title: Resource Plugin for Rollouts
authors:
  - '@aburan28'
  - '@Hariharasuthan99'
sponsors:
  - '@zaller'
creation-date: 2025-08-03
---

# Resource Plugin for Rollouts

This document proposes a plugin-based architecture for extending Argo Rollouts to support advanced deployment strategies (canary and blue/green) for various Kubernetes resource types beyond Deployments, with an initial focus on StatefulSets.

## Summary

Currently, Argo Rollouts only supports Deployment workload types, which internally manage ReplicaSets. This proposal aims to extend Argo Rollouts to support other Kubernetes resources through a plugin architecture using a **new dedicated RolloutPlugin Controller** that manages a new CRD.

This approach creates a clean separation from the existing controller and allows for resource-specific optimizations while providing a framework that the community can extend for additional resource types.

## Motivation

The current Rollouts controller is tightly coupled to ReplicaSets, which limits support to Deployment workloads only. Many users need to perform canary or blue-green upgrades on other workload types such as StatefulSets, DaemonSets, and potentially custom resources. By implementing a plugin architecture, we can support these resource types without requiring major rewrites of the controller for each new resource.

StatefulSets present a compelling initial use case due to their prevalence in stateful application deployments. Their native update capabilities are limited to basic rolling updates with optional partition-based updates, without support for:

1. Controlled traffic distribution during updates
2. Automatic analysis and verification of updates
3. Progressive traffic shifting
4. Automated canary analysis
5. Blue/green deployments with instant rollback capability

### Goals

- Define plugin architectures that can work with any Kubernetes resource type
- Create interfaces that abstract resource-specific details while maintaining core rollout functionality
- Support canary and blue/green deployments for StatefulSets as initial implementations
- Maintain compatibility with existing Argo Rollouts functionality
- Provide a framework that the community can extend for additional resource types

### Non-Goals

- Modify the core Kubernetes controllers for any resource type
- Implement every possible resource type within this proposal
- Support in-place updates for resources that don't natively support it
- Create a monolithic solution that tries to handle all use cases internally

## Proposed Implementation: RolloutPlugin Controller

This approach involves creating a dedicated controller that manages a new Custom Resource Definition (CRD) called `RolloutPlugin`. This controller is completely separate from the existing Argo Rollouts controller, providing clean separation and resource-specific optimizations. The implementation of the new controller would be done using the [controller-runtime](https://github.com/kubernetes-sigs/controller-runtime) library instead of native informers as the library abstracts away a lot of boilerplate code that would be needed otherwise.

### Traffic Routing and Scaling Strategy

The RolloutPlugin CRD and controller focus on progressive delivery through native resource capabilities (e.g., StatefulSet partition field) rather than traffic management.

**Key Decisions:**

1. **No `setCanaryScale` field**: To avoid coupling pod count with traffic routing, the CRD will not implement the `setCanaryScale` field used in the existing Rollout CRD. This decouples the notion of replica/pod counts from traffic distribution.

2. **Weight-only approach**: The CRD will only use `setWeight` steps to control the progression of rollouts. For StatefulSets, this translates to partition field manipulation (percentage of pods updated), not traffic percentages.

3. **Future plugin extensibility**: While traffic routing is not in the initial scope, the CRD and plugin interface are designed to allow individual plugins to implement traffic routing capabilities in the future if needed for specific resource types. This keeps the core controller simple while maintaining flexibility for future enhancements.

**Example**: For a StatefulSet with 10 replicas, `setWeight: 40` means update 4 pods (40% of replicas) by setting `partition: 6`, not that 40% of traffic goes to those pods.

### RolloutPlugin CRD

The `RolloutPlugin` CRD defines a generic interface for managing the rollout of any Kubernetes resource type.

#### CRD Design Options for Resource Specification

Since the RolloutPlugin CRD needs to be generic enough to support multiple Kubernetes resource types (StatefulSets, DaemonSets, etc.), there are three potential approaches for how users specify which resource to manage:

##### Option 1: Embed Resource Specs in the CRD

This approach embeds the complete spec for each possible resource type directly in the RolloutPlugin CRD.

**Example:**
```yaml
apiVersion: argorollouts.io/v1alpha1
kind: RolloutPlugin
metadata:
  name: my-rollout
spec:
  resourceType: StatefulSet
  statefulSetSpec:
    # Complete StatefulSet spec here
  daemonSetSpec:
    # Complete DaemonSet spec here (unused)
  strategy:
    # ...
```

**Pros:**
- Self-contained: All configuration in one place, no external dependencies
- Version control friendly: Complete desired configuration captured in one resource
- Initial deployment: Can create the workload and rollout management together

**Cons:**
- CRD complexity: Very large CRD schema with multiple resource type specs
- Maintenance burden: Need to keep CRD in sync with upstream Kubernetes resource definitions
- Migration difficulty: Existing workloads would need to be recreated or have specs copied

##### Option 2: Reference Existing Kubernetes Objects (Recommended)

This approach references an already-created Kubernetes resource by name and namespace using a `workloadRef`.

**Example:**
```yaml
apiVersion: argorollouts.io/v1alpha1
kind: RolloutPlugin
metadata:
  name: my-rollout
spec:
  workloadRef:
    apiVersion: apps/v1
    kind: StatefulSet
    name: my-statefulset
    namespace: default
  strategy:
    # ...
```

**Pros:**
- Simple CRD: Minimal CRD schema, just needs reference fields
- Separation of concerns: Workload definition separate from rollout strategy
- Works with existing resources: Can add rollout capabilities to existing workloads without recreating them
- Standard Kubernetes patterns: Follows existing patterns like HPA referencing Deployments and Rollout referencing Deployments
- Easier maintenance: No need to keep CRD in sync with Kubernetes resource schemas
- Clear ownership: The original resource remains the source of truth for the workload spec

**Cons:**
- Two-step process: Must create the workload first, then the RolloutPlugin
- Coordination complexity: Updates to workload spec need coordination with rollout strategy

##### Option 3: Create Dedicated CRDs per Resource Type

This approach creates separate CRDs for each resource type: `StatefulSetRollout`, `DaemonSetRollout`, etc.

**Example:**
```yaml
apiVersion: argorollouts.io/v1alpha1
kind: StatefulSetRollout
metadata:
  name: my-sts-rollout
spec:
  statefulSetSpec:
    # StatefulSet specific fields
  strategy:
    # Rollout strategy
```

**Pros:**
- Type safety: Each CRD is strongly typed for its specific resource
- Independent evolution: Each CRD can evolve independently
- Resource-specific features: Easy to add resource-specific rollout features

**Cons:**
- Maintenance burden: Multiple CRDs to install and maintain (one per resource type)
- Code duplication: Similar controller logic needs to be replicated or abstracted
- Documentation overhead: Need separate documentation for each CRD

##### Selected Approach: workloadRef (Option 2)

This proposal adopts **Option 2 (workloadRef)** as the primary approach because:

1. **Alignment with existing patterns**: Mirrors how current Argo Rollouts uses `workloadRef` for Deployments
2. **Plugin architecture compatibility**: Controller remains resource-agnostic, delegating to plugins
3. **Minimal CRD complexity**: Keeps the CRD generic and maintainable
4. **Practical for canary deployments**: Plugins can manipulate referenced resources (e.g., partition field for StatefulSets)
5. **Practical for blue/green deployments**: Plugins can create/manage additional resources (e.g., green StatefulSet) while referencing the original as blue

#### RolloutPlugin CRD Specification

With the `workloadRef` approach, the `RolloutPlugin` CRD is defined as follows:

```yaml
apiVersion: argorollouts.io/v1alpha1
kind: RolloutPlugin
metadata:
  name: my-statefulset-rollout
spec:
  # Reference to the workload being managed
  workloadRef:
    apiVersion: apps/v1
    kind: StatefulSet
    name: my-statefulset
    namespace: default
  
  # Plugin-specific configuration
  plugin:
    name: statefulset
    verify: true
    sha256: "abc123..."
    url: "https://example.com/plugins/statefulset"
    # Plugin-specific configuration
    config:
      customField1: "value1"
      customField2: "value2"
  
  # Rollout strategy definition
  strategy:
    # Strategy configuration depends on the type
    canary:
      steps:
        - setWeight: 20
        - pause: {duration: 1h}
        - setWeight: 40
        - pause: {duration: 1h}
        - setWeight: 60
        - pause: {duration: 1h}
        - setWeight: 80
        - pause: {duration: 1h}
    # BlueGreen configuration would be here
    # blueGreen:
    #   ...
```

### RolloutPlugin Status

The `RolloutPlugin` status would track the progress of the rollout:

```yaml
status:
  observedGeneration: 1
  initialized: true
  currentRevision: "hash123456"
  updatedRevision: "hash789012"
  currentStepIndex: 2
  currentStepComplete: false
  rolloutInProgress: true
  conditions:
    - type: Progressing
      status: "True"
      message: StatefulSet successfully updated to partition 8 (20% weight)
  paused: false
  aborted: false
  # Plugin-specific status fields
  pluginStatus:
    resourceType: "StatefulSet"
    customStatusField1: "value1"
    customStatusField2: "value2"
```

### RolloutPlugin Controller Architecture

The controller would load plugins for different resource types, where each plugin implements the specific logic needed for that resource type. The controller itself would be resource-agnostic, delegating resource-specific operations to plugins. The `RolloutPlugin` controller would use HashiCorp's go-plugin library for plugin management, similar to the existing plugins in Argo Rollouts.

### Generic Plugin Interface

```go
// ResourcePhase defines the possible phases of a resource operation
type ResourcePhase string

const (
  ResourcePhaseRunning     ResourcePhase = "Running"
  ResourcePhaseSuccessful  ResourcePhase = "Successful"
  ResourcePhaseFailed      ResourcePhase = "Failed"
  ResourcePhaseError       ResourcePhase = "Error"
)

// RolloutPluginContext contains information needed by plugin operations
type RolloutPluginContext struct {
  // The RolloutPlugin resource
  RolloutPlugin *v1alpha1.RolloutPlugin
  
  // Configuration for the plugin operation
  Config map[string]interface{}
  
  // Status carries persisted state between operations
  Status map[string]interface{}
}

// OperationResult contains the result of an operation
type OperationResult struct {
  // Success indicates if the operation was successful
  Success bool
  
  // Message provides additional context about the operation
  Message string
  
  // Status contains operation-specific status information
  Status map[string]interface{}
  
  // Error details if the operation failed
  Error *RpcError
  
  // Whether to requeue the reconciliation and after how long
  Requeue bool
  RequeueAfter time.Duration
}

// RpcError is a structured error for plugin responses
type RpcError struct {
  ErrorString string
  Code int
  Details string
}

// RolloutPlugin defines the generic interface for resource plugins
type RolloutPlugin interface {
  // Initialize the plugin with any required setup
  Init() error
  
  // CreateResource creates a resource with the specified role
  // role can be "stable", "canary", "preview", "active", etc.
  CreateResource(ctx RolloutPluginContext, role string) OperationResult
  
  // UpdateResource updates a resource with the specified role
  // role can be "stable", "canary", "preview", "active", etc.
  UpdateResource(ctx RolloutPluginContext, role string) OperationResult
  
  // ScaleResource scales a resource with the specified role to the desired replica count
  ScaleResource(ctx RolloutPluginContext, role string, replicas int32) OperationResult
  
  // SetWeight updates traffic distribution between resources
  // For canary: percentage of traffic to canary
  // For blue-green: percentage of traffic to preview
  SetWeight(ctx RolloutPluginContext, desiredWeight int32) OperationResult
  
  // PromoteResource promotes a resource to become the new stable version
  // For canary: promotes canary to stable
  // For blue-green: promotes preview to active
  PromoteResource(ctx RolloutPluginContext) OperationResult
  
  // Terminate stops an uncompleted operation
  Terminate(ctx RolloutPluginContext) OperationResult
  
  // VerifyWeight checks if the desired weight distribution has been achieved
  VerifyWeight(ctx RolloutPluginContext, desiredWeight int32) OperationResult
  
  // IsResourceReady checks if a specific resource is considered ready
  IsResourceReady(ctx RolloutPluginContext, role string) OperationResult
  
  // GetPluginInfo returns metadata about the plugin
  GetPluginInfo() PluginInfo
}

// PluginInfo contains metadata about a plugin
type PluginInfo struct {
  Name string
  Version string
  SupportedResourceKinds []string
  SupportedStrategies []string
}
```

### Controller Reconciliation Flow

The `RolloutPlugin` controller would follow this reconciliation flow:

1. Fetch the `RolloutPlugin` resource
2. Initialize the appropriate plugin based on the resource type
3. Sync the plugin with the current state
4. Process steps based on the current index:
   - For `setWeight` steps, call the plugin's SetWeight method
   - For `pause` steps, handle the pause logic in the controller
   - For `analysis` steps, create and monitor analysis runs
5. Update the `RolloutPlugin` status with the current progress
6. Requeue for the next reconciliation

#### 

## StatefulSet Implementation for Both Approaches

Regardless of which approach is selected, the StatefulSet-specific implementation will leverage the partition field to achieve canary deployments:

### Canary Deployment for StatefulSets

The canary deployment will leverage the StatefulSet's partition feature:

1. Start with partition = replicas (no pods updated)
2. Gradually decrease partition as the rollout progresses:
   - partition = replicas * (1 - weight/100)
   - Examples for 10 replicas:
     - 20% weight → partition = 8 (2 pods updated)
     - 50% weight → partition = 5 (5 pods updated)
     - 100% weight → partition = 0 (all pods updated)
3. Monitor pod health and analyze metrics at each step
4. On failure, set partition back to replicas (revert all pods)
5. On success, set partition to 0 (update all pods)

Given StatefulSets update in descending order (N-1, N-2, ...), the partition field indicates the first pod that will NOT be updated. This means pods with ordinal >= partition remain at the old version.

### Blue/Green Deployment for StatefulSets

Blue/green deployment for StatefulSets is more complex due to their stateful nature. We propose the following approach:

1. Create a new StatefulSet with a different name (with suffix -green)
2. Populate state in the new StatefulSet if needed (may require application-specific logic)
3. Direct new traffic to the green StatefulSet through service selector changes
4. Analyze metrics to verify the green deployment
5. On success, switch all traffic to the green deployment and decommission the blue
6. On failure, keep traffic on the blue deployment and clean up the green

This approach may require application-specific state transfer, which can be handled through pre/post hooks or custom step plugins.

## StatefulSet Use Cases

### Use Case 1: Canary Deployment of Stateful Database

**Current Challenge**: A team runs a stateful MongoDB database as a StatefulSet in Kubernetes. When upgrading to a new MongoDB version, they currently have two unsatisfactory options:

1. Update all instances at once (risky)
2. Manually update the partition field over time (labor-intensive and error-prone)

**Proposed Solution**: Using either approach with StatefulSet support, the team can:

```yaml
# Approach 1: Using extended Rollout
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: mongodb-rollout
spec:
  replicas: 5
  workloadRef:
    apiVersion: apps/v1
    kind: StatefulSet
    name: mongodb
  
  resourcePlugin:
    name: statefulset
  
  strategy:
    canary:
      steps:
        - setWeight: 20  # Update 1 replica (partition=4)
        - pause: {duration: 2h}
        - analysis:
            templates:
              - templateName: mongodb-metrics
        - setWeight: 40  # Update 2 replicas (partition=3)
        - pause: {duration: 2h}
        - setWeight: 100 # Update all replicas (partition=0)
```

```yaml
# Approach 2: Using RolloutPlugin CRD
apiVersion: argorollouts.io/v1alpha1
kind: RolloutPlugin
metadata:
  name: mongodb-rollout
spec:
  workloadRef:
    apiVersion: apps/v1
    kind: StatefulSet
    name: mongodb
  
  plugin:
    name: statefulset
    verify: true
    sha256: "abc123..."
    url: "https://example.com/plugins/statefulset"
  
  strategy:
    type: Canary
    canary:
      steps:
        - setWeight: 20  # Update 1 replica (partition=4)
        - pause: {duration: 2h}
        - analysis:
            templates:
              - templateName: mongodb-metrics
        - setWeight: 40  # Update 2 replicas (partition=3)
        - pause: {duration: 2h}
        - setWeight: 100 # Update all replicas (partition=0)
```

### Use Case 2: Blue/Green Deployment of Stateful Message Queue

**Current Challenge**: A team runs a Kafka cluster as a StatefulSet. They need to test a new version with production traffic but be able to instantly roll back if issues are detected.

**Proposed Solution**: Using either approach with StatefulSet blue/green support:

```yaml
# Approach 1: Using extended Rollout
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: kafka-rollout
spec:
  replicas: 3
  workloadRef:
    apiVersion: apps/v1
    kind: StatefulSet
    name: kafka
  
  resourcePlugin:
    name: statefulset
  
  strategy:
    blueGreen:
      activeService: kafka-active
      previewService: kafka-preview
      autoPromotionEnabled: false
      previewReplicaCount: 3
      scaleDownDelaySeconds: 600
      analysis:
        templates:
          - templateName: kafka-metrics
```

```yaml
# Approach 2: Using RolloutPlugin CRD
apiVersion: argorollouts.io/v1alpha1
kind: RolloutPlugin
metadata:
  name: kafka-rollout
spec:
  workloadRef:
    apiVersion: apps/v1
    kind: StatefulSet
    name: kafka
  
  plugin:
    name: statefulset
    verify: true
    sha256: "abc123..."
    url: "https://example.com/plugins/statefulset"
  
  strategy:
    blueGreen:
      activeService: kafka-active
      previewService: kafka-preview
      autoPromotionEnabled: false
      previewReplicaCount: 3
      scaleDownDelaySeconds: 600
      analysis:
        templates:
          - templateName: kafka-metrics
```

## ReplicaSet Use Cases

To demonstrate the flexibility of the RolloutPlugin approach, here are simpler use cases for ReplicaSets.

### Use Case 1: Simple Canary Deployment with ReplicaSets

**Current Challenge**: A team wants a lightweight way to progressively roll out changes to their service without using full Deployments, with controlled increments and automatic analysis between steps.

**Proposed Solution**:

```yaml
apiVersion: argorollouts.io/v1alpha1
kind: RolloutPlugin
metadata:
  name: simple-canary-rs
spec:
  workloadRef:
    apiVersion: apps/v1
    kind: Deployment
    name: canary-app
  
  plugin:
    name: Deployment
    verify: true
    sha256: "abc123..."
    url: "https://example.com/plugins/replicaset"
    config:
      revisionHistoryLimit: 3
  
  strategy:
    type: Canary
    canary:
      steps:
        - setWeight: 20
        - pause: {duration: 10m}
        - analysis:
            templates:
              - templateName: success-rate
        - setWeight: 40
        - pause: {duration: 10m}
        - setWeight: 60
        - analysis:
            templates:
              - templateName: success-rate
        - setWeight: 100
```

### Use Case 2: Simple Blue/Green Deployment with ReplicaSets

**Current Challenge**: A team wants a simple way to implement blue/green deployments for a service that requires zero-downtime updates with a preview environment.

**Proposed Solution**:

```yaml
apiVersion: argorollouts.io/v1alpha1
kind: RolloutPlugin
metadata:
  name: bluegreen-rs
spec:
  workloadRef:
    apiVersion: apps/v1
    kind: Deployment
    name: bluegreen-app
  
  plugin:
    name: Deployment
    verify: true
    sha256: "abc123..."
    url: "https://example.com/plugins/replicaset"
  
  strategy:
    type: BlueGreen
    blueGreen:
      activeService: bluegreen-active
      previewService: bluegreen-preview
      autoPromotionEnabled: false
      prePromotionAnalysis:
        templates:
          - templateName: http-success-rate
      scaleDownDelaySeconds: 300
```

These use cases demonstrate how the RolloutPlugin approach can directly manage ReplicaSets to implement controlled rollout strategies. The plugin architecture enables precise control over the ReplicaSet lifecycle while maintaining the core rollout functionality.

## Advantages of the RolloutPlugin Controller Approach

- **Clean separation from existing controller**: No risk of affecting existing Rollout functionality
- **Resource-specific optimizations**: Can be optimized specifically for different resource types
- **Easier to implement resource-specific features**: Dedicated controller allows for resource-specific features without complicating the core Rollouts controller
- **Independent evolution**: Can evolve independently from the existing Rollouts controller
- **Clear plugin boundaries**: Well-defined interface for plugin developers

## Security Considerations

- Resource plugins run with the same permissions as the controller
- Consider limiting plugin scope with RBAC
- Validate plugin binaries with checksums

## Risks and Mitigations

1. **Risk**: Data loss during StatefulSet updates
   **Mitigation**: Clear documentation and guidance on PVC handling

2. **Risk**: Service disruption during updates
   **Mitigation**: Conservative default settings and thorough analysis templates

3. **Risk**: Plugin stability affecting controller
   **Mitigation**: Proper error handling and isolation in plugin execution

## Conclusion

This proposal defines plugin architectures for extending Argo Rollouts to support multiple resource types, with an initial focus on StatefulSets. This approach provides a way to manage advanced deployment strategies for various Kubernetes resources, allowing users to apply consistent rollout patterns across their entire infrastructure.

The StatefulSet implementation demonstrates how existing Kubernetes capabilities (the partition field) can be leveraged to provide advanced deployment strategies, while this architecture also ensures that future resource types can be supported with minimal changes to existing code.

## References

1. Kubernetes StatefulSet documentation: <https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/>
2. Kubernetes StatefulSet update strategies: <https://kubernetes.io/docs/tutorials/stateful-application/basic-stateful-set/#updating-statefulsets>
3. Argo Rollouts plugin architecture: <https://argoproj.github.io/argo-rollouts/plugins/>
4. HashiCorp go-plugin library: <https://github.com/hashicorp/go-plugin>

## Archive: Alternative Approach Considered

### Approach 1: Resource Plugin Extension (Not Selected)

This approach was considered but not selected. It would have extended the existing Rollouts controller with a resource plugin system, allowing the current Rollout CRD to manage different resource types.

**Why Not Selected:**

- Higher risk of affecting existing functionality
- Potentially more complex controller code
- Tighter coupling with existing Rollouts implementation

**Advantages:**

- Unified user experience with existing Rollouts
- Single controller to maintain
- Reuses existing CLI commands and UI

#### Generic Resource Plugin Interface

The core of this approach was a plugin interface with methods generic enough to work with any Kubernetes resource type and support both canary and blue-green deployment strategies:

```go
// ResourcePhase defines the possible phases of a resource operation
type ResourcePhase string

const (
  ResourcePhaseRunning     ResourcePhase = "Running"
  ResourcePhaseSuccessful  ResourcePhase = "Successful"
  ResourcePhaseFailed      ResourcePhase = "Failed"
  ResourcePhaseError       ResourcePhase = "Error"
)

// ResourceContext contains information and configuration for resource plugin operations
type ResourceContext struct {
  // Plugin name
  PluginName string
  
  // Configuration for the plugin operation
  Config map[string]interface{}
  
  // Status carries persisted state between operations
  Status map[string]interface{}
}

// ResourceStatus represents the status of a resource plugin operation
type ResourceStatus struct {
  // Resource identifier (e.g. hash of the spec)
  ResourceId string
  
  // The current phase of the operation
  Phase ResourcePhase
  
  // Human-readable message about the operation
  Message string
  
  // Start time of the operation
  StartedAt *metav1.Time
  
  // Finish time of the operation
  FinishedAt *metav1.Time
  
  // Resource-specific status information (persisted between operations)
  Status map[string]interface{}
  
  // Wait time before checking status again for incomplete operations
  RequeueAfter time.Duration
}

// ResourceResult contains the result of a plugin operation
type ResourceResult struct {
  // The resulting phase of the operation
  Phase ResourcePhase
  
  // Human-readable message about the operation result
  Message string
  
  // Resource-specific status information to persist
  Status map[string]interface{}
  
  // Wait time before executing the operation again for incomplete operations
  RequeueAfter time.Duration
}

type ResourcePlugin interface {
  // Initialize the plugin
  Init() error
  
  // CreateResource creates or ensures a resource with the given role exists
  // role can be "stable", "canary", "preview", "active", etc.
  CreateResource( rollout *v1alpha1.Rollout, resourceContext *ResourceContext, role string) (*ResourceResult, error)
  
  // UpdateResource updates a resource with the given role
  // role can be "stable", "canary", "preview", "active", etc.
  UpdateResource( rollout *v1alpha1.Rollout, resourceContext *ResourceContext, role string) (*ResourceResult, error)
  
  // ScaleResource scales a resource with the given role to the desired replica count
  ScaleResource( rollout *v1alpha1.Rollout, resourceContext *ResourceContext, role string, replicas int32) (*ResourceResult, error)
  
  // SetWeight updates the traffic weight between resources
  // For canary: percentage of traffic to canary
  // For blue-green: percentage of traffic to preview
  SetWeight( rollout *v1alpha1.Rollout, resourceContext *ResourceContext, desiredWeight int32) (*ResourceResult, error)
  
  // SwitchTrafficRoute changes the routing of traffic between resources
  // Particularly useful for blue-green to route traffic to preview
  SwitchTrafficRoute( rollout *v1alpha1.Rollout, resourceContext *ResourceContext, options map[string]string) (*ResourceResult, error)
  
  // PromoteResource promotes a resource to become the new stable/active version
  // For canary: promotes canary to stable
  // For blue-green: promotes preview to active
  PromoteResource( rollout *v1alpha1.Rollout, resourceContext *ResourceContext) (*ResourceResult, error)
  
  // Terminate stops an uncompleted operation 
  Terminate( rollout *v1alpha1.Rollout, resourceContext *ResourceContext) (*ResourceResult, error)
  
  // VerifyWeight verifies if the desired weight distribution has been achieved
  VerifyWeight( rollout *v1alpha1.Rollout, resourceContext *ResourceContext, desiredWeight int32) (bool, error)
  
  // IsResourceReady returns whether a specific resource is considered ready
  IsResourceReady( rollout *v1alpha1.Rollout, resourceContext *ResourceContext, role string) (bool, error)
  
  // Type returns the type/name of the resource plugin
  Type() string
  
  // SupportsStrategy returns whether the plugin supports a given deployment strategy
  SupportsStrategy(strategy StrategyType) bool
}
```

#### Extended Rollout Spec

The Rollout spec would be extended with a resourcePlugin field:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: statefulset-rollout
spec:
  replicas: 5
  workloadRef:
    apiVersion: apps/v1
    kind: StatefulSet
    name: my-statefulset
  
  resourcePlugin:
    name: statefulset
    config:
      # Optional plugin-specific configuration
      volumeClaimTemplates:
        - metadata:
            name: data
          spec:
            accessModes: ["ReadWriteOnce"]
            resources:
              requests:
                storage: 1Gi
  
  strategy:
    # Existing strategy remains unchanged
    canary:
      steps:
        - setWeight: 20
        - pause: {duration: 1h}
        - setWeight: 40
        - pause: {duration: 1h}
```

#### Rollout Status Extensions

The Rollout status would be extended with a resourcePluginStatus field:

```yaml
status:
  canary:
    resourcePluginStatus:
      - name: statefulset
        message: "Updated partition to 3 (60%)"
        phase: Running
        startedAt: "2025-08-03T12:00:00Z"
        status:
          partition: 3
          currentReplicas: 3
          updatedReplicas: 2
```

#### Example Use Cases for Approach 1

##### StatefulSet Canary with Extended Rollout

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: mongodb-rollout
spec:
  replicas: 5
  workloadRef:
    apiVersion: apps/v1
    kind: StatefulSet
    name: mongodb
  
  resourcePlugin:
    name: statefulset
  
  strategy:
    canary:
      steps:
        - setWeight: 20  # Update 1 replica (partition=4)
        - pause: {duration: 2h}
        - analysis:
            templates:
              - templateName: mongodb-metrics
        - setWeight: 40  # Update 2 replicas (partition=3)
        - pause: {duration: 2h}
        - setWeight: 100 # Update all replicas (partition=0)
```

##### StatefulSet Blue/Green with Extended Rollout

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: kafka-rollout
spec:
  replicas: 3
  workloadRef:
    apiVersion: apps/v1
    kind: StatefulSet
    name: kafka
  
  resourcePlugin:
    name: statefulset
  
  strategy:
    blueGreen:
      activeService: kafka-active
      previewService: kafka-preview
      autoPromotionEnabled: false
      previewReplicaCount: 3
      scaleDownDelaySeconds: 600
      analysis:
        templates:
          - templateName: kafka-metrics
```

##### ReplicaSet Canary with Extended Rollout

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: simple-replicaset-canary
spec:
  replicas: 10
  
  resourcePlugin:
    name: replicaset
    config:
      revisionHistoryLimit: 3
  
  selector:
    matchLabels:
      app: canary-app
  
  template:
    metadata:
      labels:
        app: canary-app
    spec:
      containers:
      - name: app
        image: myapp:v2
        ports:
        - containerPort: 8080
  
  strategy:
    canary:
      steps:
      - setWeight: 20  # Create canary ReplicaSet with 2 replicas (20% of 10)
      - pause: {duration: 10m}
      - analysis:
          templates:
            - templateName: success-rate
      - setWeight: 40  # Scale canary ReplicaSet to 4 replicas (40% of 10)
      - pause: {duration: 10m}
      - setWeight: 60  # Scale canary ReplicaSet to 6 replicas (60% of 10) 
      - analysis:
          templates:
            - templateName: success-rate
      - setWeight: 100 # Scale canary ReplicaSet to 10 replicas, scale down stable
```

##### ReplicaSet Blue/Green with Extended Rollout

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: bluegreen-replicaset
spec:
  replicas: 5
  
  resourcePlugin:
    name: replicaset
  
  selector:
    matchLabels:
      app: bluegreen-app
  
  template:
    metadata:
      labels:
        app: bluegreen-app
    spec:
      containers:
      - name: app
        image: myapp:v2
        ports:
        - containerPort: 8080
  
  strategy:
    blueGreen:
      activeService: bluegreen-active
      previewService: bluegreen-preview
      autoPromotionEnabled: false
      prePromotionAnalysis:
        templates:
          - templateName: http-success-rate
      scaleDownDelaySeconds: 300
```
