# InfluxDB Metrics

An [InfluxDB](https://www.influxdata.com/) query using [Flux](https://docs.influxdata.com/influxdb/cloud/query-data/get-started/query-influxdb/) can be used to obtain measurements for analysis.

```yaml
apiVersion: argoproj.io/v1alpha1
kind: AnalysisTemplate
metadata:
  name: error-rate
spec:
  args:
  - name: application-name
  metrics:
  - name: error-rate
    # NOTE: To be consistent with the prometheus metrics provider InfluxDB query results are returned as an array.
    # In the example we're looking at index 0 of the returned array to obtain the value we're using for the success condition
    successCondition: result[0] <= 0.01
    provider:
      influxdb:
        profile: my-influxdb-secret  # optional, defaults to 'influxdb'
        query: |
          from(bucket: "app_istio")
            |> range(start: -15m)
            |> filter(fn: (r) => r["destination_workload"] == "{{ args.application-name }}")
            |> filter(fn: (r) => r["_measurement"] == "istio:istio_requests_errors_percentage:rate1m:5xx")

```

An InfluxDB access profile can be configured using a Kubernetes secret in the `argo-rollouts` namespace. Alternate accounts can be used by creating more secrets of the same format and specifying which secret to use in the metric provider configuration using the `profile` field.

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: influxdb
type: Opaque
stringData:
  address: <infuxdb-url>
  authToken: <influxdb-auth-token>
  org: <influxdb-org>
```
