# Prometheus Metrics

A [Prometheus](https://prometheus.io/) query can be used to obtain measurements for analysis.

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
    # NOTE: prometheus queries return results in the form of a vector.
    # So it is common to access the index 0 of the returned array to obtain the value
    successCondition: result[0] >= 0.95
    failureLimit: 3
    provider:
      prometheus:
        address: http://prometheus.example.com:9090
        query: |
          sum(irate(
            istio_requests_total{reporter="source",destination_service=~"{{args.service-name}}",response_code!~"5.*"}[5m]
          )) /
          sum(irate(
            istio_requests_total{reporter="source",destination_service=~"{{args.service-name}}"}[5m]
          ))
```

The example shows Istio metrics, but you can use any kind of metric available to your prometheus instance. We suggest
you validate your [PromQL expression](https://prometheus.io/docs/prometheus/latest/querying/basics/) using the [Prometheus GUI first](https://prometheus.io/docs/introduction/first_steps/#using-the-expression-browser).

See the [Analysis Overview page](../../features/analysis) for more details on the available options.

# Additional Metadata

Any additional metadata from the Prometheus controller, like the resolved queries after substituting the template's
arguments, etc. will appear under the `Metadata` map in the `MetricsResult` object of `AnalysisRun`.
