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
  name: wavefront-api-tokens
type: Opaque
stringData:
  example1.wavefront.com: <token1>
  example2.wavefront.com: <token2>
```
