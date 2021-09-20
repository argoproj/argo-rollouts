# Graphite Metrics

A [Graphite](https://graphiteapp.org/) query can be used to obtain measurements for analysis.

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
    # Note that the Argo Rollouts Graphite metrics provider returns results as an array of float64s with 6 decimal places.
    successCondition: results[0] >= 90.000000
    failureLimit: 3
    provider:
      graphite:
        address: http://graphite.example.com:9090
        query: |
          target=summarize(
            asPercent(
              sumSeries(
                stats.timers.httpServerRequests.app.{{args.service-name}}.exception.*.method.*.outcome.{CLIENT_ERROR,INFORMATIONAL,REDIRECTION,SUCCESS}.status.*.uri.*.count
              ),
              sumSeries(
                stats.timers.httpServerRequests.app.{{args.service-name}}.exception.*.method.*.outcome.*.status.*.uri.*.count
              )
            ),
            '5min',
            'avg'
          )
```
