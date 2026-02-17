# New Relic Metrics

Argo Rollouts integrates with [New Relic](https://newrelic.com) to automate canary analysis, ensuring new versions of your application meet quality expectations before a full deployment. This section explains how to configure analysis templates to monitor **response time** and **error rate** using New Relic and how to incorporate these templates into a Rollout definition.

## Web Provider

The generic `web` provider can interact with New Relic over HTTPS. An analysis template dynamically constructs the query based on provided arguments, and the results are evaluated against predefined success criteria. The templates below allow monitoring of key metrics, such as response time and error rate, by comparing the canary (new version) and stable (existing version) deployments.

### How It Works

1. **Initialization and Parameterization**: Each template accepts parameters such as `appName`, `rolloutName`, timing configurations (`analysisInitialDelay`, `analysisInterval`, `analysisTimeWindow`), and optional custom conditions for the New Relic query, using `customConditionPrefix`, `customConditionValue`, and `customConditionSuffix`. These parameters customize the query and define the conditions under which the analysis will run. 

2. **Authentication**: The templates require the New Relic account ID and personal API key (also known as the user API key), provided via a Kubernetes secret, for authentication:
  
      ```yaml
      apiVersion: v1
      kind: Secret
      metadata:
        name: newrelic
      type: Opaque
      stringData:
        personal-api-key: <newrelic-personal-api-key>
        account-id: <newrelic-account-id>
      ```

      The secret must be defined in each namespace where a Rollout is executed, rather than in the namespace where the Argo controller runs.

3. **Data Retrieval and Analysis**: The query is written in the New Relic Query Language ([NRQL](https://docs.newrelic.com/docs/nrql/get-started/introduction-nrql-new-relics-query-language/)) and sent to New Relic's GraphQL API via a POST request. The query consists of three nested subqueries. The innermost query retrieves the most frequently used transaction names (i.e. endpoints), with the maximum number of names specified by the `limitOfTransactionNames` parameter. The next level calculates error rates or average response times for both the stable and canary deployments for each transaction name. The outermost query computes the deviations between the canary and stable deployments for each transaction name.

4. **Conditions Evaluation**: The analysis determines the success of the canary deployment by checking whether the deviations fall within acceptable thresholds. If they do, the rollout proceeds. If not, the template returns an `Inconclusive` status, causing the Rollout to pause and require manual intervention.

5. **Automated Execution**: Both templates are designed to run at regular intervals, repeating the analysis multiple times to ensure consistent results before making a decision on the deployment's success.

### Template for Response Time Verification

This template compares the response times of the canary and stable deployments. The analysis calculates the deviation in response time between the two versions and checks if it falls within an acceptable threshold.

The threshold (`responseTimeDeviationThreshold`) is expressed as a multiple of the standard deviation of the stable deployment's response time, with the default being six standard deviations. This approach ensures that when response times are naturally variable, the tolerance is proportionally higher, while a more stable response time demands a stricter threshold.

The resolution component (`responseTimeResolution`) is used to prevent division by zero when calculating the deviation. It is a small value added to the denominator to avoid division by zero when the stable standard deviation is zero. The resolution also helps to prevent analysis failure due to small variations in the response time.

```yaml
kind: ClusterAnalysisTemplate
apiVersion: argoproj.io/v1alpha1
metadata:
  name: new-relic.canary.response-time.verification
spec:
  args:
  - name: appName
  - name: rolloutName
  - name: analysisInitialDelay
    value: "4m"  # Initial delay before the analysis starts (use a duration in the Argo Rollouts format)
  - name: analysisInterval
    value: "1m"  # Interval between subsequent analysis executions (use a duration in the Argo Rollouts format)
  - name: analysisTimeWindow
    value: "70 seconds"  # Time window for the data query to New Relic (use a duration in the NRQL format)
  - name: analysisCount
    value: "60"  # Number of times the analysis will run
  - name: responseTimeDeviationThreshold
    value: "6.0"  # Threshold for acceptable response time deviation (in multiples of standard deviation)
  - name: responseTimeResolution
    value: "0.3"  # Resolution component for the response time calculation (in milliseconds)
  - name: limitOfTransactionNames
    value: "16"  # Maximum number of endpoints to analyze
  - name: inconclusiveLimit
    value: 3  # Maximum allowed inconclusive executions before marking the entire analysis as inconclusive
  - name: customConditionPrefix
    value: ""  # Prefix of custom query condition, e.g. to filter by a custom tag: "and tags.DC = '"
  - name: customConditionValue
    value: ""  # Value of custom query condition, e.g. the value of the tag: "us-east1"
  - name: customConditionSuffix
    value: ""  # Suffix of custom query condition, usually a closing apostrophe: "'"
  - name: stablePodHash
  - name: latestPodHash
  - name: new-relic.personal-api-key
    valueFrom:
      secretKeyRef:
        name: newrelic
        key: personal-api-key
  - name: new-relic.account-id
    valueFrom:
      secretKeyRef:
        name: newrelic
        key: account-id
  metrics:
  - name: "NR Canary Response Time"
    successCondition: "len(result) > 0 && all(result, {.responseTimeDeviation < {{ args.responseTimeDeviationThreshold }}})"
    failureCondition: "false"
    initialDelay: "{{ args.analysisInitialDelay }}"
    interval: "{{ args.analysisInterval }}"
    count: "{{ args.analysisCount }}"
    inconclusiveLimit: "{{ args.inconclusiveLimit }}"
    provider:
      web:
        method: POST
        url: "https://api.newrelic.com/graphql"
        timeoutSeconds: 120
        headers:
          - key: Content-Type
            value: "application/json"
          - key: API-Key
            value: "{{ args.new-relic.personal-api-key }}"
        jsonPath: "{$.data.actor.account.nrql.results}"
        jsonBody:
          query: |
              {
                actor {
                  account(id: {{ args.new-relic.account-id }}) {
                    nrql(
                      timeout: 120
                      query: """
                              select
                                  average(abs(`canary` - `stable`) / (`stdev` + {{ args.responseTimeResolution }})) as `responseTimeDeviation`,
                                  average(`canary`) as `canary`, average(`stable`) as `stable`, average(`stdev`) as `stdev`
                              from
                                  (
                                      select
                                          (filter(
                                              average(`apm.service.transaction.duration`),
                                              where
                                                host like '{{ args.rolloutName }}-{{ args.stablePodHash }}%'
                                          ) or -1.0) as `stable`,
                                          (filter(
                                              average(`apm.service.transaction.duration`),
                                              where
                                                host like '{{ args.rolloutName }}-{{ args.latestPodHash }}%'
                                          ) or -1.0) as `canary`,
                                          filter(
                                              stddev(`apm.service.transaction.duration`),
                                              where
                                                host like '{{ args.rolloutName }}-{{ args.stablePodHash }}%'
                                          ) as `stdev`
                                      from
                                          Metric
                                      where
                                          appName = '{{ args.appName }}' {{ args.customConditionPrefix }}{{ args.customConditionValue }}{{ args.customConditionSuffix }}
                                          and transactionName in (
                                              select
                                                  transactionName
                                              from
                                                  (
                                                      FROM
                                                          Metric
                                                      SELECT
                                                          count(`apm.service.transaction.duration`) FACET transactionName
                                                      WHERE
                                                          appName = '{{ args.appName }}' {{ args.customConditionPrefix }}{{ args.customConditionValue }}{{ args.customConditionSuffix }}
                                                      LIMIT
                                                          {{ args.limitOfTransactionNames }}
                                                  )
                                          ) FACET transactionName
                                  ) FACET transactionName since {{ args.analysisTimeWindow }} ago
                      """
                    ) {
                      results
                    }
                  }
                }
              }
          variables: ""
```

### Template for Error Rate Verification

This template monitors the error rate, another crucial metric for assessing canary deployments. It calculates the error rate deviation between the canary and stable deployments and checks if it falls within an acceptable threshold.

The threshold (`errorRateDeviationThreshold`) is expressed as a multiple of the stable version's error rate, with the default being two times that rate.

The resolution component (`errorRateResolution`) prevents division by zero when calculating the deviation. It adds a small value to the denominator to avoid division by zero when the stable version's error rate is zero, and also helps prevent analysis failure due to very small error rates.

```yaml
kind: ClusterAnalysisTemplate
apiVersion: argoproj.io/v1alpha1
metadata:
  name: new-relic.canary.error-rate.verification
spec:
  args:
  - name: appName
  - name: rolloutName
  - name: analysisInitialDelay
    value: "4m"  # Initial delay before the analysis starts (use a duration in the Argo Rollouts format)
  - name: analysisInterval
    value: "1m"  # Interval between subsequent analysis executions (use a duration in the Argo Rollouts format)
  - name: analysisTimeWindow
    value: "70 seconds"  # Time window for the data query to New Relic (use a duration in the NRQL format)
  - name: analysisCount
    value: "60"  # Number of times the analysis will run
  - name: errorRateDeviationThreshold
    value: "2.0"  # Threshold for acceptable error rate deviation (in multiples of existing error rate)
  - name: errorRateResolution
    value: "0.0001"  # Resolution component for the error rate calculation
  - name: limitOfTransactionNames
    value: "16"  # Maximum number of endpoints to analyze
  - name: inconclusiveLimit
    value: 3  # Maximum allowed inconclusive executions before marking the entire analysis as inconclusive
  - name: customConditionPrefix
    value: ""  # Prefix of custom query condition, e.g. to filter by a custom tag: "and tags.DC = '"
  - name: customConditionValue
    value: ""  # Value of custom query condition, e.g. the value of the tag: "us-east1"
  - name: customConditionSuffix
    value: ""  # Suffix of custom query condition, usually a closing apostrophe: "'"
  - name: stablePodHash
  - name: latestPodHash
  - name: new-relic.personal-api-key
    valueFrom:
      secretKeyRef:
        name: newrelic
        key: personal-api-key
  - name: new-relic.account-id
    valueFrom:
      secretKeyRef:
        name: newrelic
        key: account-id
  metrics:
  - name: "NR Canary Error Rate"
    successCondition: "all(result, {.errorRateDeviation < {{ args.errorRateDeviationThreshold }}})"
    failureCondition: "false"
    initialDelay: "{{ args.analysisInitialDelay }}"
    interval: "{{ args.analysisInterval }}"
    count: "{{ args.analysisCount }}"
    inconclusiveLimit: "{{ args.inconclusiveLimit }}"
    provider:
      web:
        method: POST
        url: "https://api.newrelic.com/graphql"
        timeoutSeconds: 120
        headers:
          - key: Content-Type
            value: "application/json"
          - key: API-Key
            value: "{{ args.new-relic.personal-api-key }}"
        jsonPath: "{$.data.actor.account.nrql.results}"
        jsonBody:
          query: |
              {
                actor {
                  account(id: {{ args.new-relic.account-id }}) {
                    nrql(
                      timeout: 120
                      query: """
                              select
                                  average(`canary` / (`stable` + {{ args.errorRateResolution }})) as `errorRateDeviation`,
                                  average(`canary`) as `canary`, average(`stable`) as `stable`
                              from
                                  (
                                      select
                                          (filter(
                                              count(`apm.service.transaction.error.count`) / count(`apm.service.transaction.duration`),
                                              where
                                                host like '{{ args.rolloutName }}-{{ args.stablePodHash }}%'
                                          ) or 0) as `stable`,
                                          (filter(
                                              count(`apm.service.transaction.error.count`) / count(`apm.service.transaction.duration`),
                                              where
                                                host like '{{ args.rolloutName }}-{{ args.latestPodHash }}%'
                                          ) or 0) as `canary`
                                      from
                                          Metric
                                      where
                                          appName = '{{ args.appName }}' {{ args.customConditionPrefix }}{{ args.customConditionValue }}{{ args.customConditionSuffix }}
                                          and transactionName in (
                                              select
                                                  transactionName
                                              from
                                                  (
                                                      FROM
                                                          Metric
                                                      SELECT
                                                          count(`apm.service.transaction.duration`) FACET transactionName
                                                      WHERE
                                                          appName = '{{ args.appName }}' {{ args.customConditionPrefix }}{{ args.customConditionValue }}{{ args.customConditionSuffix }}
                                                      LIMIT
                                                          {{ args.limitOfTransactionNames }}
                                                  )
                                          ) FACET transactionName
                                  ) FACET transactionName since {{ args.analysisTimeWindow }} ago
                      """
                    ) {
                      results
                    }
                  }
                }
              }
          variables: ""
```

### Example Rollout Definition Incorporating New Relic Analysis

To integrate the above templates into your Rollout, use the following example Rollout definition. This definition specifies how the canary analysis will be executed as part of the deployment strategy.

```yaml
metadata:
  labels:
    application: my-app
    cluster: cluster-name-1
    region: us-east1
spec:
  strategy:
    canary:
      steps:
        - setWeight: 1
        - analysis:
            analysisRunMetadata: {}
            args:
              - name: metricName
                value: 'Automatic canary verification'
              - name: appName
                valueFrom:
                  fieldRef:
                    fieldPath: metadata.labels['application']
              - name: rolloutName
                valueFrom:
                  fieldRef:
                    fieldPath: metadata.name
              - name: customConditionPrefix
                value: "and tags.DC = '"
              - name: customConditionValue
                valueFrom:
                  fieldRef:
                    fieldPath: metadata.labels['region']
              - name: customConditionSuffix
                value: "'"
              - name: stablePodHash
                valueFrom:
                  podTemplateHashValue: Stable
              - name: latestPodHash
                valueFrom:
                  podTemplateHashValue: Latest
            templates:
              - clusterScope: true
                templateName: new-relic.canary.response-time.verification
              - clusterScope: true
                templateName: new-relic.canary.error-rate.verification
        - setWeight: 100
```

## New Relic Provider

!!! important
    Available since v0.10.0

An alternative `newRelic` provider can be used to query New Relic data:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: ClusterAnalysisTemplate
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
        timeout: 10 # NRQL query timeout in seconds. Optional, defaults to 5
```

The `result` evaluated for the condition will always be a map or list of maps. The name follows the pattern `function` or `function.field`; for example, `SELECT average(duration) FROM Transaction` will yield `average.duration`. In such cases, the field result cannot be accessed using dot notation and should instead be accessed like `result['average.duration']`. Query results can be renamed using the [NRQL `AS` clause](https://docs.newrelic.com/docs/nrql/nrql-syntax-clauses-functions/#sel-as), as demonstrated above.

A New Relic access profile can be configured using a Kubernetes secret in the namespace where the `argo-rollouts` controller operates, rather than in the namespaces where the Rollouts are executed (note that this differs from the `web` provider described earlier). To use alternate accounts, you can create additional secrets following the same format and specify the appropriate secret in the metric provider configuration using the `profile` field.

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: newrelic
type: Opaque
stringData:
  personal-api-key: <newrelic-personal-api-key>
  account-id: <newrelic-account-id>
  region: "us" # optional, defaults to "us" if not set. Only set to "eu" if you use EU New Relic
```

To use the New Relic metric provider from behind a proxy, provide a `base-url-rest` key pointing to the base URL of the New Relic REST API for your proxy, and a `base-url-nerdgraph` key pointing to the base URL for NerdGraph for your proxy:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: newrelic
type: Opaque
stringData:
  personal-api-key: <newrelic-personal-api-key>
  account-id: <newrelic-account-id>
  region: "us" # optional, defaults to "us" if not set. Only set to "eu" if you use EU New Relic
  base-url-rest: <your-base-url>
  base-url-nerdgraph: <your-base-url>
```

### Additional Metadata

The New Relic provider returns the below metadata under the `Metadata` map in the `MetricsResult` object of `AnalysisRun`.

| KEY                   | Description |
|-----------------------|-------------|
| ResolvedNewRelicQuery | Resolved query after substituting the template's arguments |

## Conclusion

By integrating New Relic with Argo Rollouts, you can automate the verification of critical quality metrics like response time and error rate for canary deployments. These templates provide a robust and reusable framework for ensuring that new versions of your application meet quality expectations before being fully rolled out. This integration enhances your continuous delivery pipeline, reducing the risk associated with deploying changes to production.
