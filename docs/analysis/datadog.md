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
        apiVersion: v2
        interval: 5m
        query: |
          sum:requests.error.rate{service:{{args.service-name}}}
```

The field `apiVersion` refers to the API version of Datadog (v1 or v2). Default value is `v1` if this is omitted.

!!! note
    Datadog is moving away from the legacy v1 API. Rate limits imposed by Datadog are therefore stricter when using v1. It is recommended to switch to v2 soon. If you switch to v2, you will not be able to use formulas (operations between individual queries).

Datadog api and app tokens can be configured in a kubernetes secret in argo-rollouts namespace.

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: datadog
type: Opaque
stringData:
  address: https://api.datadoghq.com
  api-key: <datadog-api-key>
  app-key: <datadog-app-key>
```

`apiVersion` here is different from the `apiVersion` from the Datadog configuration above.
