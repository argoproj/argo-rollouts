# Apache SkyWalking Metrics

!!! important
    Available since v1.5.0

A [SkyWalking](https://skywalking.apache.org/) query using GraphQL can be used to obtain measurements for analysis.

```yaml
apiVersion: argoproj.io/v1alpha1
kind: AnalysisTemplate
metadata:
  name: apdex
spec:
  args:
  - name: service-name
  metrics:
  - name: apdex
    interval: 5m
    successCondition: "all(result.service_apdex.values.values, {asFloat(.value) >= 9900})"
    failureLimit: 3
    provider:
      skywalking:
        interval: 3m
        address: http://skywalking-oap.istio-system:12800
        query: |
          query queryData($duration: Duration!) {
            service_apdex: readMetricsValues(
              condition: { name: "service_apdex", entity: { scope: Service, serviceName: "{{ args.service-name }}", normal: true } },
              duration: $duration) {
                label values { values { value } }
              }
          }
```

The `result` evaluated for the query depends on the specific GraphQL you give, you can try to run the GraphQL query first and inspect the output format, then compose the condition.
