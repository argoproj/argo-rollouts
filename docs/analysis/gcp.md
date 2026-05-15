# Google Cloud Monitoring Metrics

A [Google Cloud Monitoring](https://cloud.google.com/monitoring) query can be used to obtain measurements for analysis. Both [PromQL](https://cloud.google.com/stackdriver/docs/managed-prometheus/query) and structured [filter](https://cloud.google.com/monitoring/api/v3/filters) queries are supported; exactly one of `query` or `filter` must be set.

## Setup

The provider uses [Application Default Credentials](https://cloud.google.com/docs/authentication/application-default-credentials). On GKE the recommended setup is [Workload Identity](https://cloud.google.com/kubernetes-engine/docs/how-to/workload-identity):

1. Create a Google service account with `roles/monitoring.viewer` on the project whose metrics you want to query.
2. Bind the argo-rollouts service account to it:
   ```bash
   gcloud iam service-accounts add-iam-policy-binding $GSA \
     --role=roles/iam.workloadIdentityUser \
     --member="serviceAccount:$GKE_PROJECT.svc.id.goog[argo-rollouts/argo-rollouts]"
   ```
3. Annotate the argo-rollouts service account:
   ```bash
   kubectl -n argo-rollouts annotate sa argo-rollouts \
     iam.gke.io/gcp-service-account=$GSA --overwrite
   ```

Outside GKE, set `GOOGLE_APPLICATION_CREDENTIALS` to a key file or run `gcloud auth application-default login` for local development.

## Configuration

- `project` — GCP project ID to query. Required.
- `interval` — lookback window. Optional, defaults to `5m`.
- `query` — PromQL expression. Mutually exclusive with `filter`.
- `filter` — Cloud Monitoring metric filter. Mutually exclusive with `query`.
- `aggregation` — alignment and reduction options used with `filter`. Optional.
- `timeout` — deadline for a single Cloud Monitoring API call, in seconds. Optional, defaults to `30`. Must be non-negative. Raise this for queries with large cross-project aggregations that can legitimately exceed 30s.

## Exmaples

### PromQL

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
    successCondition: "all(result[0], # >= 0.95)"
    failureLimit: 0
    provider:
      gcp:
        project: my-project
        interval: 5m
        query: |
          sum(rate({"__name__"="istio.io/service/server/request_count","destination_service_name"="{{args.service-name}}","response_code"!~"5.*"}[1m]))
          /
          sum(rate({"__name__"="istio.io/service/server/request_count","destination_service_name"="{{args.service-name}}"}[1m]))
```

### Filter

For queries that don't map well to PromQL, use a [filter](https://cloud.google.com/monitoring/api/v3/filters) with an optional [aggregation](https://cloud.google.com/monitoring/api/v3/aggregation):

```yaml
apiVersion: argoproj.io/v1alpha1
kind: AnalysisTemplate
metadata:
  name: cpu-utilization
spec:
  metrics:
  - name: cpu-utilization
    successCondition: result[0][0] < 0.8
    failureLimit: 0
    provider:
      gcp:
        project: my-project
        interval: 5m
        timeout: 60 # API-call deadline in seconds. Optional, defaults to 30.
        filter: |
          metric.type="compute.googleapis.com/instance/cpu/utilization"
          AND resource.labels.zone="us-central1-a"
        aggregation:
          alignmentPeriod: 60s
          perSeriesAligner: ALIGN_MEAN
          crossSeriesReducer: REDUCE_MEAN
          groupByFields:
          - resource.label.zone
```

`perSeriesAligner` and `crossSeriesReducer` accept the [`Aligner`](https://cloud.google.com/monitoring/api/ref_v3/rest/v3/projects.alertPolicies#Aligner) and [`Reducer`](https://cloud.google.com/monitoring/api/ref_v3/rest/v3/projects.alertPolicies#Reducer) enum names.

## Result shape

Both query modes return `[][]float64` — the outer slice is the list of time series, the inner slice is the sample values within `interval`. Common conditions:

- `all(result[0], # >= 0.95)` — every sample in the first series passes (typical canary check)
- `result[0][0] >= 0.95` — first sample of the first series (only meaningful when the query collapses to one point)
- `result[0][len(result[0])-1]` — most recent sample

## Additional Metadata

The GCP provider returns the following metadata under the `Metadata` map in the `MetricsResult` object of `AnalysisRun`.

| KEY               | Description                                                          |
|-------------------|----------------------------------------------------------------------|
| ResolvedGCPQuery  | Resolved PromQL query after substituting the template's arguments    |
| ResolvedGCPFilter | Resolved filter expression after substituting the template's arguments |
| warnings          | Warnings returned by the PromQL endpoint or `ExecutionErrors` returned by ListTimeSeries |
