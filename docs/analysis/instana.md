# IBM Instana Metrics

IBM [Instana](https://www.ibm.com/products/instana) metrics can be used to obtain measurements for analysis. Both **application monitoring** metrics (call throughput, latency, error rates) and **infrastructure monitoring** metrics (CPU, memory, JVM, etc.) are supported.

```yaml
apiVersion: argoproj.io/v1alpha1
kind: AnalysisTemplate
metadata:
  name: error-rate
spec:
  args:
  - name: service-name
  metrics:
  - name: error-rate
    interval: 1m
    successCondition: default(result, 0) < 0.01
    failureLimit: 3
    provider:
      instana:
        metricType: application
        metricId: calls.erroneous.rate
        query: "entity.selfType:APPLICATION AND entity.application.name:{{args.service-name}}"
        aggregation: mean
        rollupInterval: 60
```

## Secret Configuration

Instana credentials are stored in a Kubernetes Secret. By default the Secret must be named **`instana`** and exist in the `argo-rollouts` namespace.

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: instana
type: Opaque
stringData:
  endpoint: https://<unit-name>.instana.io   # Your Instana backend URL
  api-token: <instana-api-token>              # API token with read access to metrics
```

### Credential lookup order

1. **Named secret via `secretRef`** — if `secretRef.name` is set in the `AnalysisTemplate`, Argo Rollouts looks for that secret in the appropriate namespace.
2. **Environment variables** — `INSTANA_ENDPOINT` and `INSTANA_API_TOKEN`.
3. **Default secret** — a secret named `instana` in the namespace where Argo Rollouts is deployed.

### Namespaced secret

To use a secret that lives in the same namespace as the `AnalysisTemplate` (rather than the `argo-rollouts` namespace), set `secretRef.namespaced: true`:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: AnalysisTemplate
metadata:
  name: error-rate
  namespace: my-app
spec:
  metrics:
  - name: error-rate
    interval: 1m
    successCondition: default(result, 0) < 0.01
    failureLimit: 3
    provider:
      instana:
        metricType: application
        metricId: calls.erroneous.rate
        aggregation: mean
        secretRef:
          name: my-instana-secret
          namespaced: true
```

## Field Reference

| Field             | Required | Description |
|-------------------|----------|-------------|
| `metricType`      | ✓        | `application` or `infrastructure` |
| `metricId`        | ✓        | The Instana metric identifier (e.g. `calls.latency.p99`, `cpu.user`) |
| `query`           |          | [Dynamic Focus](https://www.ibm.com/docs/en/instana-observability/current?topic=instana-filtering-dynamic-focus) query to scope the metric |
| `aggregation`     |          | Aggregation function: `mean` (default), `sum`, `min`, `max`, `p25`, `p50`, `p75`, `p90`, `p95`, `p98`, `p99` |
| `rollupInterval`  |          | Aggregation window in seconds (default: `60`) |
| `secretRef.name`  |          | Name of the Kubernetes secret holding credentials |
| `secretRef.namespaced` |     | If `true`, look up the secret in the AnalysisTemplate's namespace |

## Metric Types

### Application Monitoring

Application metrics are sourced from the Instana application monitoring API. Common metric IDs:

| Metric ID | Description |
|-----------|-------------|
| `calls.count` | Total number of calls |
| `calls.erroneous.count` | Number of erroneous calls |
| `calls.erroneous.rate` | Error rate (erroneous calls / total calls) |
| `calls.latency.mean` | Mean call latency (ms) |
| `calls.latency.p50` | p50 call latency (ms) |
| `calls.latency.p99` | p99 call latency (ms) |

```yaml
provider:
  instana:
    metricType: application
    metricId: calls.latency.p99
    query: "entity.application.name:my-service"
    aggregation: mean
    rollupInterval: 60
```

### Infrastructure Monitoring

Infrastructure metrics are sourced from the Instana infrastructure monitoring API. Common metric IDs:

| Metric ID | Description |
|-----------|-------------|
| `cpu.user` | CPU user utilisation (%) |
| `cpu.sys` | CPU system utilisation (%) |
| `memory.used` | Memory used (bytes) |
| `jvm.memory.heap.used` | JVM heap memory used (bytes) |

```yaml
provider:
  instana:
    metricType: infrastructure
    metricId: cpu.user
    query: "entity.type:host AND entity.zone:production"
    aggregation: mean
    rollupInterval: 60
```

## Tips

### Handling empty results

Instana may return empty results when no data is available in the queried window. Use the `default()` function to treat an empty result as a safe value:

```yaml
successCondition: default(result, 0) < 0.05
```

### Canary analysis with error rate

A typical canary analysis template checking that the error rate stays below 1%:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: AnalysisTemplate
metadata:
  name: canary-error-rate
spec:
  args:
  - name: service-name
  metrics:
  - name: error-rate
    interval: 2m
    successCondition: default(result, 0) < 0.01
    failureCondition: default(result, 0) >= 0.05
    failureLimit: 3
    provider:
      instana:
        metricType: application
        metricId: calls.erroneous.rate
        query: "entity.application.name:{{args.service-name}}"
        aggregation: mean
        rollupInterval: 120
```
