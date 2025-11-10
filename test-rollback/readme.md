# Argo Rollouts Performance Testing Environment

Set up a test environment for verifying Argo Rollouts.

## Prerequisites

- Kubernetes cluster
- Helm v3
- kubectl
- kubectl argo rollouts plugin

## Setup Steps

### Start Argo rollout controller:
```
go run ./cmd/rollouts-controller/main.go
```

### Install Prometheus

```bash
# Add Prometheus helm repo
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update

# Install kube-prometheus-stack
helm install prometheus prometheus-community/kube-prometheus-stack \
  --namespace monitoring \
  --create-namespace \
  --set prometheus.prometheusSpec.serviceMonitorSelectorNilUsesHelmValues=false
```

### Delete any existing load test job
```bash
kubectl delete job k6-load-test --ignore-not-found
```

### Apply the rollout manifest
```bash
kubectl apply -f test-rollback/example-rollout.yaml
```

### Watch the rollout
```bash
kubectl argo rollouts get rollout example-rollout --watch
```

### Prometheus UI
```bash
kubectl port-forward svc/prometheus-operated -n monitoring 9090:9090
```

### Metrics endpoint
```bash
kubectl port-forward $(kubectl get pod -l app=example-app -o jsonpath='{.items[0].metadata.name}') 9113:9113
```

### Trigger rollout revision
```bash
kubectl argo rollouts set image example-rollout example-app=nginx:1.21.0
```

### Trigger rollback
```bash
kubectl argo rollouts undo example-rollout
```

### Watch rollback
```bash
kubectl argo rollouts get rollout example-rollout --watch
```

### Measure rollback performance
```bash
chmod +x test-rollback/measure-rollback.sh
./test-rollback/measure-rollback.sh
```

## Useful Commands

### Launch Argo Rollouts dashboard
```bash
kubectl argo rollouts dashboard
```
Then navigate to rollouts UI - http://localhost:3100/rollouts/rollout/default/example-rollout

### Check analysis runs
```bash
kubectl argo rollouts list analysisrun
```

### View detailed rollout status
```bash
kubectl argo rollouts status example-rollout
```

### View prometheus targets
```bash
open http://localhost:9090/targets
```

### Verify metrics directly
```bash
curl http://localhost:9113/metrics
```

## The rollout-canary-example.yaml includes:
- Rollout definition with canary strategy
- Nginx configuration
- Prometheus monitoring setup
- Load testing job
- Service and metric exposure
- Analysis templates

# Cleanup
```
# Remove test application
kubectl delete -f rollout-canary-example.yaml

# Uninstall Prometheus
helm uninstall prometheus -n monitoring
```