# Datadog Metrics

!!! important
    Available since v0.10.0

A [Datadog](https://www.datadoghq.com/) query can be used to obtain measurements for analysis.

```yaml
apiVersion: argoproj.io/v1alpha1
kind: AnalysisTemplate
metadata:
  name: loq-error-rate
spec:
  args:
  - name: service-name
  metrics:
  - name: error-rate
    interval: 5m
    successCondition: result <= 0.01
    failureLimit: 3
    provider:
      datadog:
        interval: 5m
        query: |
          sum:requests.error.count{service:{{args.service-name}}} /
          sum:requests.request.count{service:{{args.service-name}}}
```

Datadog api and app tokens can be configured in a kubernetes secret in argo-rollouts namespace.

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: datadog-secret
type: Opaque
data:
  ARGO_ROLLOUTS_DD_ADDRESS: https://api.datadoghq.com
  ARGO_ROLLOUTS_DD_API_KEY: <datadog-api-key>
  ARGO_ROLLOUTS_DD_APP_KEY: <datadog-app-key>
```

In Rollout Deployment add the follow env source

```yaml
spec:
  containers:
  - env:
    - name: ARGO_ROLLOUTS_DD_ADDRESS
      valueFrom:
        secretKeyRef:
          key: ARGO_ROLLOUTS_DD_ADDRESS
          name: datadog-secret
    - name: ARGO_ROLLOUTS_DD_APP_KEY
      valueFrom:
        secretKeyRef:
          key: ARGO_ROLLOUTS_DD_APP_KEY
          name: datadog-secret
    - name: ARGO_ROLLOUTS_DD_API_KEY
      valueFrom:
        secretKeyRef:
          key: ARGO_ROLLOUTS_DD_API_KEY
          name: datadog-secret
    image: quay.io/argoproj/argo-rollouts:<version>
      ```
