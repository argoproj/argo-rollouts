# Migrating to Rollouts

Migrating to Argo Rollouts involves converting an existing Deployment resource, to a Rollout resource.

When converting a Deployment to a Rollout, it involves changing three fields:

1. Replacing the `apiVersion` from `apps/v1` to `argoproj.io/v1alpha1`
1. Replacing the `kind` from `Deployment` to `Rollout`
1. Replacing the deployment strategy with a [blue-green](features/bluegreen.md) or [canary](features/canary.md) strategy

Below is an example of a Rollout resource using the canary strategy.

```yaml
apiVersion: argoproj.io/v1alpha1  # Changed from apps/v1
kind: Rollout                     # Changed from Deployment
metadata:
  name: rollouts-demo
spec:
  selector:
    matchLabels:
      app: rollouts-demo
  template:
    metadata:
      labels:
        app: rollouts-demo
    spec:
      containers:
      - name: rollouts-demo
        image: argoproj/rollouts-demo:blue
        ports:
        - containerPort: 8080
  strategy:
    canary:                        # Changed from rollingUpdate or recreate
      steps:
      - setWeight: 20
      - pause: {}
```

## Other Considerations

When migrating a Deployment which is already serving live production traffic, a Rollout should
run next to the Deployment before deleting the Deployment. **Not following this approach might result in 
downtime**. It also allows for the Rollout to be tested before deleting the original Deployment.
