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

### Namespace Watching

To watch only specific namespace, add to deployment args:
```yaml
args:
- --namespace=argo-rollouts
- --rolloutplugin-metrics-port=8082
- --rolloutplugin-healthz-port=8083
```

### Leader Election

To enable leader election, add:
```yaml
args:
- --leader-elect=true
- --rolloutplugin-metrics-port=8082
- --rolloutplugin-healthz-port=8083
```

### Log Level

To change log level:
```yaml
args:
- --loglevel=debug  # or info, warn, error
- --rolloutplugin-metrics-port=8082
- --rolloutplugin-healthz-port=8083
```

## Testing the RolloutPlugin

### 1. Create a Test StatefulSet

```bash
kubectl apply -f test/rolloutplugin/test-statefulset.yaml
```

### 2. Create a RolloutPlugin CR

```bash
kubectl apply -f test/rolloutplugin/test-rolloutplugin.yaml
```

### 3. Trigger a Rollout

Update the StatefulSet image:
```bash
kubectl set image statefulset/test-sts busybox=quay.io/prometheus/busybox:glibc -n argo-rollouts
```

### 4. Watch the Rollout Progress

```bash
# Watch RolloutPlugin status
kubectl get rolloutplugin -n argo-rollouts -w

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
