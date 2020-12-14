# NewRelic Metrics

!!! important
    Available since v0.10.0

A [New Relic](https://newrelic.com/) query using [NRQL](https://docs.newrelic.com/docs/query-your-data/nrql-new-relic-query-language/get-started/introduction-nrql-new-relics-query-language) can be used to obtain measurements for analysis.  

```yaml
apiVersion: argoproj.io/v1alpha1
kind: AnalysisTemplate
metadata:
  name: success-rate
spec:
  args:
  - name: application-name
  metrics:
  - name: success-rate
    successCondition: result.successRate >= 0.95
    provider:
      newRelic:
        profile: my-newrelic-secret  # optional, defaults to 'newrelic'
        query: |
          FROM Transaction SELECT percentage(count(*), WHERE httpResponseCode != 500) as successRate where appName = '{{ args.application-name }}'
```

The `result` evaluated for the condition will always be map or list of maps. The name will follow the pattern of either `function` or `function.field`, e.g. `SELECT average(duration) from Transaction` will yield `average.duration`. In this case the field result cannot be accessed with dot notation and instead should be accessed like `result['average.duration']`. Query results can be renamed using the [NRQL clause `AS`](https://docs.newrelic.com/docs/query-your-data/nrql-new-relic-query-language/get-started/nrql-syntax-clauses-functions#sel-as) as seen above.

A New Relic access profile can be configured using a Kubernetes secret in the `argo-rollouts` namespace. Alternate accounts can be used by creating more secrets of the same format and specifying which secret to use in the metric provider configuration using the `profileSecretName` field. 

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: newrelic
type: Opaque
data:
  personal-api-key: <newrelic-personal-api-key>
  account-id: <newrelic-account-id>
  region: "us" # optional, defaults to "us" if not set. Only set to "eu" if you use EU New Relic
```
