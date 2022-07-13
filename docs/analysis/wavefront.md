# Wavefront Metrics

A [Wavefront](https://www.wavefront.com/) query can be used to obtain measurements for analysis.

```yaml
apiVersion: argoproj.io/v1alpha1
kind: AnalysisTemplate
metadata:
  name: success-rate
spec:
  args:
  - name: service-name
  metrics:
  - name: success-rate
    interval: 5m
    successCondition: result >= 0.95
    failureLimit: 3
    provider:
      wavefront:
        address: example.wavefront.com
        query: |
          sum(rate(
            5m, ts("istio.requestcount.count", response_code!=500 and destination_service="{{args.service-name}}"
          ))) /
          sum(rate(
            5m, ts("istio.requestcount.count", reporter=client and destination_service="{{args.service-name}}"
          )))
```

Wavefront api tokens can be configured in a kubernetes secret in argo-rollouts namespace.

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: wavefront-secret
type: Opaque
data:
  ARGO_ROLLOUTS_WAVEFRONT_ADDRESS: <address>
  ARGO_ROLLOUTS_WAVEFRONT_TOKEN: <token>
```

In Rollout Deployment add the follow env source

```yaml
spec:
  containers:
  - env:
    - name: ARGO_ROLLOUTS_WAVEFRONT_ADDRESS
      valueFrom:
        secretKeyRef:
          key: ARGO_ROLLOUTS_WAVEFRONT_ADDRESS
          name: wavefront-secret
    - name: ARGO_ROLLOUTS_WAVEFRONT_TOKEN
      valueFrom:
        secretKeyRef:
          key: ARGO_ROLLOUTS_WAVEFRONT_TOKEN
          name: wavefront-secret
    image: quay.io/argoproj/argo-rollouts:<version>
```
