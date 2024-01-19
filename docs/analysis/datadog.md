# Datadog Metrics

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

The field `apiVersion` refers to the API version of Datadog (v1 or v2). Default value is `v1` if this is omitted. See "Working with Datadog API v2" below for more information.

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

### Working with Datadog API v2

!!! important
    While some basic v2 functionality is working in earlier versions, the new properties of `formula` and `queries` are only available as of v1.7

#### Moving to v2

If your old v1 was just a simple metric query - no formula as part of the query - then you can just move to v2 by updating the `apiVersion` in your existing Analysis Template, and everything should work.

If you have a formula, you will need to update how you configure your metric. Here is a before/after example of what your Analysis Template should look like:

Before:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: AnalysisTemplate
metadata:
  name: log-error-rate
spec:
  args:
  - name: service-name
  metrics:
  - name: error-rate
    interval: 30s
    successCondition: default(result, 0) < 10
    failureLimit: 3
    provider:
      datadog:
        apiVersion: v1
        interval: 5m
        query: "moving_rollup(sum:requests.errors{service:{{args.service-name}}}.as_count(), 60, 'sum') / sum:requests{service:{{args.service-name}}}.as_count()"
```

After:

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
    # Polling rate against the Datadog API
    interval: 30s
    successCondition: default(result, 0) < 10
    failureLimit: 3
    provider:
      datadog:
        apiVersion: v2
        # The window of time we are looking at in DD. Basically we will fetch data from (now-5m) to now.
        interval: 5m
        queries:
          a: sum:requests.errors{service:{{args.service-name}}}.as_count()
          b: sum:requests{service:{{args.service-name}}}.as_count()
        formula: "moving_rollup(a, 60, 'sum') / b"
```

#### Examples

Simple v2 query with no formula

```yaml
apiVersion: argoproj.io/v1alpha1
kind: AnalysisTemplate
metadata:
  name: canary-container-restarts
spec:
  args:
    # This is set in rollout using the valueFrom: podTemplateHashValue functionality
    - name: canary-hash
    - name: service-name
    - name: restarts.initial-delay
      value: "60s"
    - name: restarts.max-restarts
      value: "4"
  metrics:
    - name: kubernetes.containers.restarts
      initialDelay: "{{ args.restarts.initial-delay }}"
      interval: 15s
      failureCondition: default(result, 0) > {{ args.restarts.max-restarts }}
      failureLimit: 0
      provider:
        datadog:
          apiVersion: v2
          interval: 5m
          queries:
            # The key is arbitrary - you will use this key to refer to the query if you use a formula.
            q: "max:kubernetes.containers.restarts{service-name:{{args.service-name}},rollouts_pod_template_hash:{{args.canary-hash}}}"
```

### Tips

#### Datadog Results

Datadog queries can return empty results if the query takes place during a time interval with no metrics. The Datadog provider will return a `nil` value yielding an error during the evaluation phase like:

```
invalid operation: < (mismatched types <nil> and float64)
```

However, empty query results yielding a `nil` value can be handled using the `default()` function. Here is a succeeding example using the `default()` function:

```yaml
successCondition: default(result, 0) < 0.05
```

#### Metric aggregation (v2 only)

By default, Datadog analysis run is configured to use `last` metric aggregator when querying Datadog v2 API. This value can be overriden by specifying a new `aggregator` value from a list of supported aggregators (`avg,min,max,sum,last,percentile,mean,l2norm,area`) for the V2 API ([docs](https://docs.datadoghq.com/api/latest/metrics/#query-scalar-data-across-multiple-products)).

For example, using count-based distribution metric (`count:metric{*}.as_count()`) with values `1,9,3,7,5` in a given `interval` will make `last` aggregator return `5`. To return a sum of all values (`25`), set `aggregator: sum` in Datadog provider block and use `moving_rollup()` function to aggregate values in the specified rollup interval. These functions can be combined in a `formula` to perform additional calculations:

```yaml
...<snip>
  metrics:
  - name: error-percentage
    interval: 30s
    successCondition: default(result, 0) < 5
    failureLimit: 3
    provider:
      datadog:
        apiVersion: v2
        interval: 5m
        aggregator: sum # override default aggregator
        queries:
          a: count:requests.errors{service:my-service}.as_count()
          b: count:requests{service:my-service}.as_count()
        formula: "moving_rollup(a, 300, 'sum') / moving_rollup(b, 300, 'sum') * 100" # percentage of requests with errors
```

#### Templates and Helm

Helm and Argo Rollouts both try to parse things between `{{ ... }}` when rendering templates. If you use Helm to deliver your manifests, you will need to escape `{{ args.whatever }}`. Using the example above, here it is set up for Helm:

```yaml
...<snip>
metrics:
  - name: kubernetes.containers.restarts
      initialDelay: "{{ `{{ args.restarts.initial-delay }}` }}"
    interval: 15s
      failureCondition: default(result, 0) > {{ `{{ args.restarts.max-restarts }}` }}
    failureLimit: 0
    provider:
      datadog:
        apiVersion: v2
        interval: 5m
        queries:
          q: "{{ `max:kubernetes.containers.restarts{kube_app_name:{{args.kube_app_name}},rollouts_pod_template_hash:{{args.canary-hash}}}` }}"
```

#### Rate Limits

For the `v1` API, you ask for an increase on the `api/v1/query` route.

For the `v2` API, the Ratelimit-Name you ask for an increase in is the `query_scalar_public`.
