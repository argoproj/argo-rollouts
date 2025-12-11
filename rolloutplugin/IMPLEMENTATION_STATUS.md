# Rollout Plugin Implementation - Built-in Mode

## Summary of Changes

Successfully implemented built-in plugin mode for the StatefulSet rollout plugin, following the exact pattern used by metric providers in Argo Rollouts.

## Files Modified/Created

### 1. **Plugin Wrapper** (`rolloutplugin/plugin/plugin.go`)
   - **Purpose**: Wraps RPC plugin interface to enable built-in mode
   - **Key Components**:
     - `RolloutPluginWrapper` struct (renamed from ResourcePluginWrapper for consistency)
     - `NewRolloutPlugin()` - For built-in plugins compiled into controller
     - `NewRpcPlugin()` - For external plugins loaded from executables
     - Wrapper methods with context support and error conversion:
       - `Init()`
       - `GetResourceStatus(ctx, workloadRef)`
       - `SetWeight(ctx, workloadRef, weight)`
       - `VerifyWeight(ctx, workloadRef, weight)`
       - `Promote(ctx, workloadRef)`
       - `Abort(ctx, workloadRef)`
   
   **Pattern**: Converts RPC types (`types.RpcError`) to standard Go errors for controller use.

### 2. **Controller Main** (`cmd/rolloutplugin-controller/main.go`)
   - **Added Imports**:
     ```go
     log "github.com/sirupsen/logrus"
     "github.com/argoproj/argo-rollouts/rolloutplugin/plugins/statefulset"
     pluginPackage "github.com/argoproj/argo-rollouts/rolloutplugin/plugin"
     ```
   
   - **Plugin Registration** (Built-in Mode):
     ```go
     logrusCtx := log.WithField("plugin", "statefulset")
     statefulSetPlugin := statefulset.NewPlugin(kubeClientset, logrusCtx)
     wrappedPlugin := pluginPackage.NewRolloutPlugin(statefulSetPlugin)
     pluginManager.RegisterPlugin("statefulset-plugin", wrappedPlugin)
     ```
   
   **Result**: Plugin runs in the same process as the controller, no external executable needed.

### 3. **VSCode Settings** (`.vscode/settings.json`)
   - **Added**: gopls directory filter to exclude `cmd/` subdirectories from analysis
   - **Purpose**: Prevents IDE errors when cmd/ contains separate main package
   - **Configuration**:
     ```json
     "gopls": {
         "build.directoryFilters": [
             "-rolloutplugin/plugins/*/cmd"
         ]
     }
     ```

### 4. **Cleanup**
   - Removed old `rolloutplugin/plugins/statefulset/main.go` (conflicted with cmd/main.go)
   - Kept `rolloutplugin/plugins/statefulset/cmd/main.go` for optional external mode

## Architecture

### Built-in Mode (Default - Implemented)
```
Controller Process
├── Plugin Manager
│   └── "statefulset-plugin"
│       └── RolloutPluginWrapper
│           └── StatefulSet Plugin
│               └── Kubernetes Client
└── Reconciler
    └── Uses Plugin via Manager
```

**Flow**:
1. Controller starts and creates plugin manager
2. StatefulSet plugin instantiated directly with Kubernetes client
3. Plugin wrapped with `RolloutPluginWrapper` for interface compatibility
4. Plugin registered with manager as "statefulset-plugin"
5. Reconciler calls plugin methods via manager
6. Wrapper converts between RPC types and standard errors

### External Mode (Optional - Available)
```
Controller Process                    External Process
├── Plugin Manager                   ├── cmd/main.go
│   └── RolloutPluginWrapper         └── StatefulSet Plugin
│       └── RPC Client ←──────Unix Socket──────→ RPC Server
└── Reconciler                           └── Kubernetes Client
```

**Note**: External mode is available via `cmd/main.go` but not used by default.

## Benefits of Built-in Mode

1. **Simpler Deployment**: No separate plugin binary to manage
2. **Better Performance**: No inter-process communication overhead
3. **Easier Debugging**: Single process, unified logs
4. **Consistent with Argo Rollouts**: Matches metric provider pattern
5. **Maintains RPC Interface**: Can switch to external mode if needed

## Terminology Updates

Changed all references from "Resource Plugin" to "Rollout Plugin" for consistency with Argo Rollouts naming conventions:
- `ResourcePluginWrapper` → `RolloutPluginWrapper`
- `NewResourcePlugin()` → `NewRolloutPlugin()`
- Error messages updated to reference "rollout plugin"

## Build Verification

✅ **Build Status**: Successful
```
Binary: bin/rolloutplugin-controller
Size: 76MB
Command: go build -o bin/rolloutplugin-controller ./cmd/rolloutplugin-controller
```

✅ **No Compilation Errors**
✅ **IDE Integration**: gopls configured to handle cmd/ subdirectories

## Testing Checklist

### Prerequisites
- [ ] Minikube cluster running
- [ ] Argo Rollouts CRDs installed
- [ ] argo-rollouts namespace created

### Test Steps
1. **Deploy Controller**:
   ```bash
   ./bin/rolloutplugin-controller --namespace=argo-rollouts
   ```

2. **Create Test StatefulSet**:
   ```yaml
   apiVersion: apps/v1
   kind: StatefulSet
   metadata:
     name: test-sts
     namespace: argo-rollouts
   spec:
     replicas: 6
     serviceName: test-sts
     selector:
       matchLabels:
         app: test
     template:
       metadata:
         labels:
           app: test
       spec:
         containers:
         - name: nginx
           image: nginx:1.19
   ```

3. **Create RolloutPlugin CR**:
   ```yaml
   apiVersion: argoproj.io/v1alpha1
   kind: RolloutPlugin
   metadata:
     name: test-rollout
     namespace: argo-rollouts
   spec:
     pluginName: statefulset-plugin
     workloadRef:
       name: test-sts
       kind: StatefulSet
   ```

4. **Test Canary Progression**:
   - Set weight to 33% (expect partition=4)
   - Set weight to 66% (expect partition=2)
   - Set weight to 100% (expect partition=0)
   - Test Promote (partition→0)
   - Test Abort (partition→replicas)

### Expected Results
- Controller starts without errors
- Plugin registered successfully
- StatefulSet partition updates according to weight formula:
  ```
  partition = replicas - (replicas × weight / 100)
  ```
- Logs show plugin operations
- No RPC communication errors (built-in mode)

## Code Quality

✅ **Follows Argo Rollouts Patterns**: Mirrors metricproviders/plugin/plugin.go
✅ **Error Handling**: Proper conversion from RPC errors to Go errors
✅ **Context Support**: All methods accept context for cancellation
✅ **Logging**: Uses logrus for consistent logging
✅ **Thread Safety**: Plugin manager uses mutexes
✅ **Interface Compliance**: Implements rolloutplugin.ResourcePlugin interface

## Next Steps

1. **Integration Testing**: Run full test suite with StatefulSet
2. **Documentation**: Update README with built-in plugin usage
3. **Metrics**: Add prometheus metrics for plugin operations
4. **Additional Plugins**: Use this pattern for other workload types (DaemonSet, Job, etc.)

## References

- **Metric Provider Pattern**: `metricproviders/plugin/plugin.go`
- **Traffic Routing Pattern**: Similar dual-mode approach
- **HashiCorp go-plugin**: https://github.com/hashicorp/go-plugin
- **Proposal**: StatefulSet canary using partition-based strategy
