# Configurable Annotations to Skip

Argo Rollouts has the ability to configure additional annotations that should be skipped when copying annotations from Rollouts to ReplicaSets. This feature allows users to prevent certain annotations from being propagated to ReplicaSets during rollout operations.

## Background

By default, Argo Rollouts skips copying some annotations from Rollouts to ReplicaSets. For example:

- `kubectl.kubernetes.io/last-applied-configuration`
- `rollout.argoproj.io/revision`
- `rollout.argoproj.io/revision-history`
- `rollout.argoproj.io/desired-replicas`
- `notified.notifications.argoproj.io`

## Configuration

You can configure additional annotations to skip by adding them to the `argo-rollouts-config` ConfigMap in the `annotationsToSkip` field.

### Format Options

The configuration supports two formats:

#### 1. YAML Array Format (Recommended)

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: argo-rollouts-config
  namespace: argo-rollouts
data:
  annotationsToSkip: |
    - "example.com/build-id"
    - "example.com/git-commit"
    - "cert-manager.io/cluster-issuer"
    - "nginx.ingress.kubernetes.io/configuration-snippet"
```

#### 2. Comma-Separated Format

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: argo-rollouts-config
  namespace: argo-rollouts
data:
  annotationsToSkip: "example.com/build-id,example.com/git-commit,cert-manager.io/cluster-issuer"
```

## Use Cases

This feature is useful if there are additional annotations specific to your environment that should not be copied from the Rollout to the underlying ReplicaSet.

## Behavior

- **Additive**: The configured annotations are added to the default list of skipped annotations
- **Startup Configuration**: Changes to the ConfigMap require a controller restart to take effect
- **Backward Compatible**: If no configuration is provided, only the default annotations are skipped
- **Graceful Degradation**: If the ConfigMap doesn't exist or has invalid configuration, the controller continues with default behavior

## Validation

### Applying Configuration Changes

**Important**: Configuration changes require a controller restart to take effect:

```bash
# After updating the ConfigMap
kubectl edit configmap argo-rollouts-config -n argo-rollouts

# Restart the controller to apply changes
kubectl rollout restart deployment/argo-rollouts -n argo-rollouts
```

### Checking Current Configuration

You can verify which annotations are currently being skipped by examining the controller logs during startup or by accessing the `annotationsToSkip` and `additionalAnnotationsToSkip` variables directly in your tests.

### Testing Configuration

Create a test Rollout with various annotations to verify the behavior:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: test-rollout
  annotations:
    # This will be skipped (configured)
    example.com/build-id: "12345"
    # This will be copied
    app.kubernetes.io/version: "v1.0.0"
    # This will be skipped (default)
    rollout.argoproj.io/revision: "1"
spec:
  replicas: 3
  strategy:
    canary:
      steps:
      - setWeight: 20
      - pause: {}
  selector:
    matchLabels:
      app: test-app
  template:
    metadata:
      labels:
        app: test-app
    spec:
      containers:
      - name: test-app
        image: nginx:1.16
```

After the Rollout creates ReplicaSets, check which annotations were copied:

```bash
kubectl get replicaset -o yaml | grep -A 10 annotations
```

## Example Configuration

Here's a example ConfigMap:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: argo-rollouts-config
  namespace: argo-rollouts
  labels:
    app.kubernetes.io/component: rollouts-controller
    app.kubernetes.io/name: argo-rollouts
    app.kubernetes.io/part-of: argo-rollouts
data:
  # Configure additional annotations to skip when copying from rollouts to replica sets
  # Note: Changes require controller restart to take effect
  annotationsToSkip: |
    - "cert-manager.io/cluster-issuer"
    - "cert-manager.io/certificate-name"
```

## Troubleshooting

### Configuration Not Taking Effect

1. **Check ConfigMap**: Ensure the ConfigMap exists in the correct namespace
   ```bash
   kubectl get configmap argo-rollouts-config -n argo-rollouts -o yaml
   ```

2. **Check Controller Logs**: Look for configuration loading messages
   ```bash
   kubectl logs -n argo-rollouts deployment/argo-rollouts -f
   ```

3. **Validate YAML**: Ensure the YAML in `annotationsToSkip` is valid
   ```bash
   echo "your-yaml-here" | yq eval '.' -
   ```

### Annotations Still Being Copied

1. **Controller Restart Required**: Configuration changes only take effect after controller restart
   ```bash
   kubectl rollout restart deployment/argo-rollouts -n argo-rollouts
   ```
2. **Case Sensitivity**: Annotation keys are case-sensitive
3. **Exact Match**: The annotation key must match exactly (no wildcards)
4. **Check Startup Logs**: Verify configuration was loaded correctly during controller startup
   ```bash
   kubectl logs -n argo-rollouts deployment/argo-rollouts | grep -i "config\|annotation"
   ```