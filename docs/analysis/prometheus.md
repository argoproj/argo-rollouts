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
        # timeout is expressed in seconds
        timeout: 40
        headers:
        - name: X-Scope-Org-ID
          value: tenant_a
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

## Utilizing Amazon Managed Prometheus

Amazon Managed Prometheus can be used as the prometheus data source for analysis. In order to do this the namespace where your analysis is running will have to have the appropriate [IRSA attached](https://docs.aws.amazon.com/prometheus/latest/userguide/AMP-onboard-ingest-metrics-new-Prometheus.html#AMP-onboard-new-Prometheus-IRSA) to allow for prometheus queries. Once you ensure the proper permissions are in place to access AMP, you can use an AMP workspace url in your ```provider``` block and add a SigV4 config for Sigv4 signing:

```yaml
provider:
  prometheus:
    address: https://aps-workspaces.$REGION.amazonaws.com/workspaces/$WORKSPACEID
    query: |
      sum(irate(
        istio_requests_total{reporter="source",destination_service=~"{{args.service-name}}",response_code!~"5.*"}[5m]
      )) /
      sum(irate(
        istio_requests_total{reporter="source",destination_service=~"{{args.service-name}}"}[5m]
      ))
    authentication:
      sigv4:
        region: $REGION
        profile: $PROFILE
        roleArn: $ROLEARN
```

## Additional Metadata

Any additional metadata from the Prometheus controller, like the resolved queries after substituting the template's
arguments, etc. will appear under the `Metadata` map in the `MetricsResult` object of `AnalysisRun`.



## Skip TLS verification

You can skip the TLS verification of the prometheus host provided by setting the options `insecure: true`.

```yaml
provider:
  prometheus:
    address: https://prometheus.example.com
    insecure: true
    query: |
      sum(irate(
        istio_requests_total{reporter="source",destination_service=~"{{args.service-name}}",response_code!~"5.*"}[5m]
      )) /
      sum(irate(
        istio_requests_total{reporter="source",destination_service=~"{{args.service-name}}"}[5m]
      ))
```
