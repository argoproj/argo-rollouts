# Application Insights Metrics

!!! important
    Available since v0.12.0

A [Azure Application Insights](https://docs.microsoft.com/en-us/azure/azure-monitor/app/app-insights-overview) query using [Kusto Query Language](https://docs.microsoft.com/en-us/azure/data-explorer/kusto/query/) can be used to obtain measurements for analysis.  

```yaml
apiVersion: argoproj.io/v1alpha1
kind: AnalysisTemplate
metadata:
  name: success-rate
spec:
  args:
  - name: url
  metrics:
  - name: success-rate
    successCondition: result.Percentage[0] >= 0.95
    provider:
      appInsights:
        profile: my-appinsights-secret  # optional, defaults to 'appinsights'
        query: | # this query will summarize how many requests succeeded over the last 5 minutes
          requests | 
          where timestamp > ago(5m) |
          where url contains "{{ args.url }}" |
          summarize Failure=count(success == False), Success=count(success == True) | 
          extend Percentage=((Success*1.0)/(Success+Failure))*100
```

The `result` evaluated for the condition will always be a map, where the key is the Column and value will always be an list even if the list only contains one value. If your query might produce multiple values, make sure to order it based on the information you want, e.g. if you want to sort by last generated `requests | sort by timestamp desc`. If you do not specify a time range in the query the default value is 12 hours.

A Application Insights access profile can be configured using a Kubernetes secret in the `argo-rollouts` namespace. Alternate accounts can be used by creating more secrets of the same format and specifying which secret to use in the metric provider configuration using the `profile` field.

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: appinsights
type: Opaque
data:
  api-id: <appinsights-api-id>
  api-key: <appinsights-api-key>
```