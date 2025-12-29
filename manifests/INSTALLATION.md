# Combined Controller Installation Guide

## What This Includes

This deployment configuration includes:
1. ✅ Updated Deployment with combined controller image
2. ✅ New ports for RolloutPlugin controller (8082, 8083)
3. ✅ Updated RBAC with StatefulSet permissions
4. ✅ Command-line flags for plugin ports
5. ✅ Service for metrics endpoints

## Files Created

1. **combined-controller-deployment.yaml** - Updated deployment manifest
2. **combined-controller-rbac.yaml** - Updated ClusterRole with RolloutPlugin permissions

## Installation Steps

### 1. Apply the RolloutPlugin CRD (if not already done)

```bash
kubectl apply -f manifests/crds/rolloutplugin-crd.yaml
```

### 2. Update RBAC Permissions

```bash
kubectl apply -f manifests/combined-controller-rbac.yaml
```

This adds:
- ✅ RolloutPlugin CRD permissions
- ✅ StatefulSet watch/update permissions

### 3. Update the Deployment

```bash
kubectl apply -f manifests/combined-controller-deployment.yaml
```

This will:
- ✅ Update to new image: `dockerhub.rnd.amadeus.net/docker-prod-dma/swb/deployment-service/argocd/argo-rollouts:latest`
- ✅ Add ports 8082 (plugin metrics) and 8083 (plugin health)
- ✅ Add command-line flags: `--rolloutplugin-metrics-port=8082` and `--rolloutplugin-healthz-port=8083`
- ✅ Set resource limits (256Mi request, 512Mi limit)

### 4. Verify the Deployment

```bash
# Check pod status
kubectl get pods -n argo-rollouts

# Check logs
kubectl logs -n argo-rollouts deployment/argo-rollouts

# Verify both controllers started
kubectl logs -n argo-rollouts deployment/argo-rollouts | grep -E "Starting|RolloutPlugin"
```

You should see:
```
INFO  Starting standard Argo Rollouts controllers
INFO  Starting RolloutPlugin controller (controller-runtime)
INFO  Registered StatefulSet plugin
INFO  All controllers started successfully
```

### 5. Verify Health Endpoints

```bash
# Port-forward to check health
kubectl port-forward -n argo-rollouts deployment/argo-rollouts 8080:8080 8081:8081 8082:8082 8083:8083

# Check standard controller health
curl http://localhost:8080/healthz

# Check RolloutPlugin controller health
curl http://localhost:8083/healthz

# Check metrics
curl http://localhost:8090/metrics  # Standard metrics
curl http://localhost:8082/metrics  # Plugin metrics
```

## Configuration Options

### Namespace Scoping

#### Cluster-Scoped Mode (Default)
By default, the combined controller watches all namespaces:
```yaml
args:
- --rolloutplugin-metrics-port=8082
- --rolloutplugin-healthz-port=8083
```

#### Namespace-Scoped Mode
To watch only a specific namespace and enable namespace-scoped RBAC:
```yaml
args:
- --namespaced
- --namespace=argo-rollouts
- --rolloutplugin-metrics-port=8082
- --rolloutplugin-healthz-port=8083
```

**Benefits of Namespace-Scoped Mode:**
- ✅ Reduced RBAC permissions (no cluster-wide access needed)
- ✅ Better security in multi-tenant environments
- ✅ Lower resource usage (fewer objects to watch)
- ✅ Leader election lease stored in the watched namespace

**Note:** In namespace-scoped mode, both the standard Argo Rollouts controllers and the RolloutPlugin controller will only watch resources in the specified namespace.

### Leader Election

#### Single-Instance Mode (Default for Development)
Leader election is enabled by default. To disable it for single-instance mode:
```yaml
args:
- --leader-elect=false
- --rolloutplugin-metrics-port=8082
- --rolloutplugin-healthz-port=8083
```

**Note:** Disabling leader election is only recommended for development or when you're certain only one controller instance is running.

#### Multi-Instance Mode with Leader Election (Recommended for Production)
Leader election is enabled by default with these parameters:
```yaml
args:
- --leader-elect=true  # Default: true
- --leader-election-lease-duration=15s  # Default: 15s
- --leader-election-renew-deadline=10s  # Default: 10s
- --leader-election-retry-period=2s     # Default: 2s
- --rolloutplugin-metrics-port=8082
- --rolloutplugin-healthz-port=8083
```

**Leader Election Behavior:**
- ✅ **Standard Controllers:** Use client-go lease-based leader election with lock `argo-rollouts-controller-lock`
- ✅ **RolloutPlugin Controller:** Use controller-runtime leader election with lock `rolloutplugin.argoproj.io`
- ✅ Both controllers share the same leader election namespace
- ✅ When an instance loses leadership, it stops reconciling but keeps running
- ✅ Only the leader instance actively reconciles resources

**Custom Leader Election Namespace:**
By default, the leader election namespace is:
- The controller namespace (if `--namespaced` mode is enabled)
- The `argo-rollouts` namespace (if cluster-scoped mode)

To override:
```yaml
args:
- --leader-election-namespace=my-namespace
- --rolloutplugin-metrics-port=8082
- --rolloutplugin-healthz-port=8083
```

**High Availability Setup:**
For production deployments, run multiple replicas with leader election enabled:
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: argo-rollouts
  namespace: argo-rollouts
spec:
  replicas: 3  # Run 3 instances for HA
  selector:
    matchLabels:
      app: argo-rollouts
  template:
    metadata:
      labels:
        app: argo-rollouts
    spec:
      containers:
      - name: argo-rollouts
        args:
        - --leader-elect=true
        - --rolloutplugin-metrics-port=8082
        - --rolloutplugin-healthz-port=8083
```

With this setup:
- Only 1 instance will be the leader and actively reconcile
- The other 2 instances will be standby replicas
- If the leader fails, another instance takes over within ~15 seconds

### Log Level

To change log level:
```yaml
args:
- --loglevel=debug  # or info, warn, error
- --rolloutplugin-metrics-port=8082
- --rolloutplugin-healthz-port=8083
```

## Testing the RolloutPlugin

### 1. Create AnalysisTemplate (Optional - for analysis support)

```bash
# Create sample analysis templates using Job provider (no Prometheus required)
kubectl apply -f - <<EOF
---
# Background analysis - 60 second sleep job
apiVersion: argoproj.io/v1alpha1
kind: AnalysisTemplate
metadata:
  name: sleep-job
  namespace: argo-rollouts
spec:
  metrics:
  - name: sleep-test
    provider:
      job:
        spec:
          backoffLimit: 0
          template:
            spec:
              containers:
              - name: sleep
                image: quay.io/prometheus/busybox:latest
                command: [sh, -c]
                args: ["sleep 60 && exit 0"]
              restartPolicy: Never
---
# Step-based analysis - 30 second sleep job
apiVersion: argoproj.io/v1alpha1
kind: AnalysisTemplate
metadata:
  name: quick-sleep-job
  namespace: argo-rollouts
spec:
  metrics:
  - name: quick-test
    provider:
      job:
        spec:
          backoffLimit: 0
          template:
            spec:
              containers:
              - name: sleep
                image: quay.io/prometheus/busybox:latest
                command: [sh, -c]
                args: ["sleep 30 && exit 0"]
              restartPolicy: Never
EOF
```

### 2. Create a Test StatefulSet

```bash
kubectl apply -f test/rolloutplugin/test-statefulset.yaml
```

### 3. Create a RolloutPlugin CR

```bash
kubectl apply -f test/rolloutplugin/test-rolloutplugin.yaml
```

### 4. Trigger a Rollout

Update the StatefulSet image:
```bash
kubectl set image statefulset/test-sts busybox=quay.io/prometheus/busybox:glibc -n argo-rollouts
```

### 5. Watch the Rollout Progress

```bash
# Watch RolloutPlugin status
kubectl get rolloutplugin -n argo-rollouts -w

# Watch AnalysisRuns (if using analysis)
kubectl get analysisrun -n argo-rollouts -w

# Watch StatefulSet
kubectl get sts -n argo-rollouts -w

# Watch controller logs
kubectl logs -n argo-rollouts deployment/argo-rollouts -f | grep RolloutPlugin
```

## Monitoring Setup

### Prometheus ServiceMonitor

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: argo-rollouts
  namespace: argo-rollouts
  labels:
    app.kubernetes.io/name: argo-rollouts
spec:
  selector:
    matchLabels:
      app.kubernetes.io/name: argo-rollouts-metrics
  endpoints:
  - port: metrics
    path: /metrics
    interval: 30s
    scrapeTimeout: 10s
  - port: plugin-metrics
    path: /metrics
    interval: 30s
    scrapeTimeout: 10s
```

Apply with:
```bash
kubectl apply -f <servicemonitor-file>.yaml
```

## AnalysisRun Support

The RolloutPlugin CRD now supports AnalysisRuns just like the standard Rollout CRD. This enables:

### Features

1. **Background Analysis (Canary)**: Continuously runs analysis during the entire rollout
2. **Step-based Analysis (Canary)**: Runs analysis at specific steps
3. **Analysis History Limits**: Configure successful/unsuccessful run retention

### Status Fields

The RolloutPlugin status now includes analysis run status for both strategies:

**Canary Status:**
- `status.canary.currentStepAnalysisRunStatus` - Current step analysis run
- `status.canary.currentBackgroundAnalysisRunStatus` - Background analysis run

### Example Usage

See the updated sample file `test/rolloutplugin/test-sts-manifests/rolloutplugin-sample.yaml` for complete examples including:
- Canary with background and step-based analysis
- Blue-Green with pre/post promotion analysis

### Monitoring Analysis

```bash
# Watch all analysis runs
kubectl get analysisrun -n argo-rollouts -w

# Get analysis run details
kubectl describe analysisrun <analysis-run-name> -n argo-rollouts

# Check RolloutPlugin status for analysis status
kubectl get rolloutplugin <name> -n argo-rollouts -o yaml
```

## Troubleshooting

### Controller Not Starting

Check logs:
```bash
kubectl logs -n argo-rollouts deployment/argo-rollouts
```

Common issues:
- Image pull errors: Verify image name and credentials
- RBAC issues: Ensure ClusterRole and ClusterRoleBinding are applied
- CRD not found: Apply RolloutPlugin CRD first

### RolloutPlugin Not Reconciling

Check:
```bash
# Verify RolloutPlugin CRD exists
kubectl get crd rolloutplugins.argoproj.io

# Check RBAC for StatefulSets
kubectl auth can-i list statefulsets --as=system:serviceaccount:argo-rollouts:argo-rollouts

# Check controller logs
kubectl logs -n argo-rollouts deployment/argo-rollouts | grep -i error
```

### Health Check Failing

Verify ports are accessible:
```bash
kubectl port-forward -n argo-rollouts deployment/argo-rollouts 8083:8083
curl http://localhost:8083/healthz
```

If timeout, check:
- Pod is running: `kubectl get pods -n argo-rollouts`
- Firewall rules
- Network policies

## Rollback Procedure

If you need to rollback to the previous version:

```bash
# Save current state
kubectl get deployment argo-rollouts -n argo-rollouts -o yaml > rollouts-combined-backup.yaml

# Rollback deployment
kubectl rollout undo deployment/argo-rollouts -n argo-rollouts

# Or apply previous manifest
kubectl apply -f manifests/install.yaml
```

## Additional Resources

- [RolloutPlugin Deployment Guide](./rolloutplugin-deployment-guide.md)
- [Approach 1 Summary](../APPROACH_1_SUMMARY.md)
- [Test Files](../test/rolloutplugin/)

## Support

For issues or questions:
1. Check controller logs
2. Verify RBAC permissions
3. Ensure CRDs are installed
4. Review the deployment guide
