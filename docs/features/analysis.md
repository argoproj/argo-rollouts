# Analysis & Progressive Delivery

Argo Rollouts provides several ways to perform analysis to drive progressive delivery.
This document describes how to achieve various forms of progressive delivery, varying the point in
time analysis is performed, it's frequency, and occurrence.

## Custom Resource Definitions

| CRD                 | Description |
|---------------------|-------------|
| Rollout             | A `Rollout` acts as a drop-in replacement for a Deployment resource. It provides additional blueGreen and canary update strategies. These strategies can create AnalysisRuns and Experiments during the update, which will progress the update, or abort it. |
| AnalysisTemplate    | An `AnalysisTemplate` is a template spec which defines *_how_* to perform a canary analysis, such as the metrics which it should perform, its frequency, and the values which are considered successful or failed. AnalysisTemplates may be parameterized with inputs values. |
| ClusterAnalysisTemplate    | A `ClusterAnalysisTemplate` is like an `AnalysisTemplate`, but it is not limited to its namespace. It can be used by any `Rollout` throughout the cluster. |
| AnalysisRun         | An `AnalysisRun` is an instantiation of an `AnalysisTemplate`. AnalysisRuns are like Jobs in that they eventually complete. Completed runs are considered Successful, Failed, or Inconclusive, and the result of the run affect if the Rollout's update will continue, abort, or pause, respectively. |
| Experiment          | An `Experiment` is limited run of one or more ReplicaSets for the purposes of analysis. Experiments typically run for a pre-determined duration, but can also run indefinitely until stopped. Experiments may reference an `AnalysisTemplate` to run during or after the experiment. The canonical use case for an Experiment is to start a baseline and canary deployment in parallel, and compare the metrics produced by the baseline and canary pods for an equal comparison. |

## Background Analysis

Analysis can be run in the background -- while the canary is progressing through its rollout steps.

The following example gradually increments the canary weight by 20% every 10 minutes until it
reaches 100%. In the background, an `AnalysisRun` is started based on the `AnalysisTemplate` named `success-rate`.
The `success-rate` template queries a prometheus server, measuring the HTTP success rates at 5
minute intervals/samples. It has no end time, and continues until stopped or failed. If the metric
is measured to be less than 95%, and there are three such measurements, the analysis is considered
Failed. The failed analysis causes the Rollout to abort, setting the canary weight back to zero,
and the Rollout would be considered in a `Degraded`. Otherwise, if the rollout completes all of its
canary steps, the rollout is considered successful and the analysis run is stopped by the controller.

This example highlights:

* Background analysis style of progressive delivery
* Using a [Prometheus](https://prometheus.io/) query to perform a measurement
* The ability to parameterize the analysis
* Delay starting the analysis run until step 3 (Set Weight 40%)

=== "Rollout"

    ```yaml
    apiVersion: argoproj.io/v1alpha1
    kind: Rollout
    metadata:
      name: guestbook
    spec:
    ...
      strategy:
        canary:
          analysis:
            templates:
            - templateName: success-rate
            startingStep: 2 # delay starting analysis run until setWeight: 40%
            args:
            - name: service-name
              value: guestbook-svc.default.svc.cluster.local
          steps:
          - setWeight: 20
          - pause: {duration: 10m}
          - setWeight: 40
          - pause: {duration: 10m}
          - setWeight: 60
          - pause: {duration: 10m}
          - setWeight: 80
          - pause: {duration: 10m}
    ```

=== "AnalysisTemplate"

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
            query: |
              sum(irate(
                istio_requests_total{reporter="source",destination_service=~"{{args.service-name}}",response_code!~"5.*"}[5m]
              )) /
              sum(irate(
                istio_requests_total{reporter="source",destination_service=~"{{args.service-name}}"}[5m]
              ))
    ```

## Inline Analysis

Analysis can also be performed as a rollout step as an inline "analysis" step. When analysis is performed
"inlined," an `AnalysisRun` is started when the step is reached, and blocks the rollout until the
run is completed. The success or failure of the analysis run decides if the rollout will proceed to
the next step, or abort the rollout completely.

This example sets the canary weight to 20%, pauses for 5 minutes, then runs an analysis. If the
analysis was successful, continues with rollout, otherwise aborts.

This example demonstrates:

* The ability to invoke an analysis in-line as part of steps

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: guestbook
spec:
...
  strategy:
    canary:
      steps:
      - setWeight: 20
      - pause: {duration: 5m}
      - analysis:
          templates:
          - templateName: success-rate
          args:
          - name: service-name
            value: guestbook-svc.default.svc.cluster.local
```

In this example, the `AnalysisTemplate` is identical to the background analysis example, but since
no interval is specified, the analysis will perform a single measurement and complete.

```yaml
apiVersion: argoproj.io/v1alpha1
kind: AnalysisTemplate
metadata:
  name: success-rate
spec:
  args:
  - name: service-name
  - name: prometheus-port
    value: 9090
  metrics:
  - name: success-rate
    successCondition: result[0] >= 0.95
    provider:
      prometheus:
        address: "http://prometheus.example.com:{{args.prometheus-port}}"
        query: |
          sum(irate(
            istio_requests_total{reporter="source",destination_service=~"{{args.service-name}}",response_code!~"5.*"}[5m]
          )) /
          sum(irate(
            istio_requests_total{reporter="source",destination_service=~"{{args.service-name}}"}[5m]
          ))
```

Multiple measurements can be performed over a longer duration period, by specifying the `count` and
`interval` fields:

```yaml hl_lines="4 5"
  metrics:
  - name: success-rate
    successCondition: result[0] >= 0.95
    interval: 60s
    count: 5
    provider:
      prometheus:
        address: http://prometheus.example.com:9090
        query: ...
```

## ClusterAnalysisTemplates

!!! important
    Available since v0.9.0

A Rollout can reference a Cluster scoped AnalysisTemplate called a
`ClusterAnalysisTemplate`. This can be useful when you want to share an AnalysisTemplate across multiple Rollouts;
in different namespaces, and avoid duplicating the same template in every namespace. Use the field
`clusterScope: true` to reference a ClusterAnalysisTemplate instead of an AnalysisTemplate.

=== "Rollout"

    ```yaml
    apiVersion: argoproj.io/v1alpha1
    kind: Rollout
    metadata:
      name: guestbook
    spec:
    ...
      strategy:
        canary:
          steps:
          - setWeight: 20
          - pause: {duration: 5m}
          - analysis:
              templates:
              - templateName: success-rate
                clusterScope: true
              args:
              - name: service-name
                value: guestbook-svc.default.svc.cluster.local
    ```

=== "ClusterAnalysisTemplate"

    ```yaml
    apiVersion: argoproj.io/v1alpha1
    kind: ClusterAnalysisTemplate
    metadata:
      name: success-rate
    spec:
      args:
      - name: service-name
      - name: prometheus-port
        value: 9090
      metrics:
      - name: success-rate
        successCondition: result[0] >= 0.95
        provider:
          prometheus:
            address: "http://prometheus.example.com:{{args.prometheus-port}}"
            query: |
              sum(irate(
                istio_requests_total{reporter="source",destination_service=~"{{args.service-name}}",response_code!~"5.*"}[5m]
              )) /
              sum(irate(
                istio_requests_total{reporter="source",destination_service=~"{{args.service-name}}"}[5m]
              ))
    ```

!!! note
    The resulting `AnalysisRun` will still run in the namespace of the `Rollout`

## Analysis with Multiple Templates

A Rollout can reference multiple AnalysisTemplates when constructing an AnalysisRun. This allows users to compose
analysis from multiple AnalysisTemplates. If multiple templates are referenced, then the controller will merge the
templates together. The controller combines the `metrics` and `args` fields of all the templates.


=== "Rollout"

    ```yaml
    apiVersion: argoproj.io/v1alpha1
    kind: Rollout
    metadata:
      name: guestbook
    spec:
    ...
      strategy:
        canary:
          analysis:
            templates:
            - templateName: success-rate
            - templateName: error-rate
            args:
            - name: service-name
              value: guestbook-svc.default.svc.cluster.local
    ```

=== "AnalysisTemplate"

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
        successCondition: result[0] >= 0.95
        failureLimit: 3
        provider:
          prometheus:
            address: http://prometheus.example.com:9090
            query: |
              sum(irate(
                istio_requests_total{reporter="source",destination_service=~"{{args.service-name}}",response_code!~"5.*"}[5m]
              )) /
              sum(irate(
                istio_requests_total{reporter="source",destination_service=~"{{args.service-name}}"}[5m]
              ))
    ---
    apiVersion: argoproj.io/v1alpha1
    kind: AnalysisTemplate
    metadata:
      name: error-rate
    spec:
      args:
      - name: service-name
      metrics:
      - name: error-rate
        interval: 5m
        successCondition: result[0] <= 0.95
        failureLimit: 3
        provider:
          prometheus:
            address: http://prometheus.example.com:9090
            query: |
              sum(irate(
                istio_requests_total{reporter="source",destination_service=~"{{args.service-name}}",response_code=~"5.*"}[5m]
              )) /
              sum(irate(
                istio_requests_total{reporter="source",destination_service=~"{{args.service-name}}"}[5m]
              ))
    ```

=== "AnalysisRun"

    ```yaml
    # NOTE: Generated AnalysisRun from the multiple templates
    apiVersion: argoproj.io/v1alpha1
    kind: AnalysisRun
    metadata:
      name: guestbook-CurrentPodHash-multiple-templates
    spec:
      args:
      - name: service-name
        value: guestbook-svc.default.svc.cluster.local
      metrics:
      - name: success-rate
        interval: 5m
        successCondition: result[0] >= 0.95
        failureLimit: 3
        provider:
          prometheus:
            address: http://prometheus.example.com:9090
            query: |
              sum(irate(
                istio_requests_total{reporter="source",destination_service=~"{{args.service-name}}",response_code!~"5.*"}[5m]
              )) /
              sum(irate(
                istio_requests_total{reporter="source",destination_service=~"{{args.service-name}}"}[5m]
              ))
      - name: error-rate
        interval: 5m
        successCondition: result[0] <= 0.95
        failureLimit: 3
        provider:
          prometheus:
            address: http://prometheus.example.com:9090
            query: |
              sum(irate(
                istio_requests_total{reporter="source",destination_service=~"{{args.service-name}}",response_code=~"5.*"}[5m]
              )) /
              sum(irate(
                istio_requests_total{reporter="source",destination_service=~"{{args.service-name}}"}[5m]
              ))
    ```

!!! note
    The controller will error when merging the templates if:

    * Multiple metrics in the templates have the same name
    * Two arguments with the same name have different default values no matter the argument value in Rollout

## Analysis Template Arguments

AnalysisTemplates may declare a set of arguments that can be passed by Rollouts. The args can then be used as in metrics configuration and are resolved at the time the AnalysisRun is created. Argument placeholders are defined as
`{{ args.<name> }}`.

```yaml
apiVersion: argoproj.io/v1alpha1
kind: AnalysisTemplate
metadata:
  name: args-example
spec:
  args:
  # required in Rollout due to no default value
  - name: service-name
  - name: stable-hash
  - name: latest-hash
  # optional in Rollout given the default value
  - name: api-url
    value: http://example/measure
  # from secret
  - name: api-token
    valueFrom:
      secretKeyRef:
        name: token-secret
        key: apiToken
  metrics:
  - name: webmetric
    successCondition: result == 'true'
    provider:
      web:
        # placeholders are resolved when an AnalysisRun is created
        url: "{{ args.api-url }}?service={{ args.service-name }}"
        headers:
          - key: Authorization
            value: "Bearer {{ args.api-token }}"
        jsonPath: "{$.results.ok}"
```

Analysis arguments defined in a Rollout are merged with the args from the AnalysisTemplate when the AnalysisRun is created.

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: guestbook
spec:
...
  strategy:
    canary:
      analysis:
        templates:
        - templateName: args-example
        args:
        # required value
        - name: service-name
          value: guestbook-svc.default.svc.cluster.local
        # override default value
        - name: api-url
          value: http://other-api
        # pod template hash from the stable ReplicaSet
        - name: stable-hash
          valueFrom:
            podTemplateHashValue: Stable
        # pod template hash from the latest ReplicaSet
        - name: latest-hash
          valueFrom:
            podTemplateHashValue: Latest
```
Analysis arguments also support valueFrom for reading metadata fields and passing them as arguments to AnalysisTemplate.
An example would be to reference metadata labels like env and region and passing them along to AnalysisTemplate.

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: guestbook
  labels:
    appType: demo-app
    buildType: nginx-app
    ...
    env: dev
    region: us-west-2
spec:
...
  strategy:
    canary:
      analysis:
        templates:
        - templateName: args-example
        args:
        ...
        - name: env
          valueFrom:
            fieldRef:
              fieldPath: metadata.labels['env']
        # region where this app is deployed
        - name: region
          valueFrom:
            fieldRef:
              fieldPath: metadata.labels['region']
```

!!! important
    Available since v1.2
Analysis arguments also support valueFrom for reading any field from Rollout status and passing them as arguments to AnalysisTemplate.
Following example references Rollout status field like aws canaryTargetGroup name and passing them along to AnalysisTemplate

from the Rollout status
```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: guestbook
  labels:
    appType: demo-app
    buildType: nginx-app
    ...
    env: dev
    region: us-west-2
spec:
...
  strategy:
    canary:
      analysis:
        templates:
        - templateName: args-example
        args:
        ...
        - name: canary-targetgroup-name
          valueFrom:
            fieldRef:
              fieldPath: status.alb.canaryTargetGroup.name
```

## BlueGreen Pre Promotion Analysis

A Rollout using the BlueGreen strategy can launch an AnalysisRun *before* it switches traffic to the new version using
pre-promotion. This can be used to block the Service selector switch until the AnalysisRun finishes successfully. The success or
failure of the AnalysisRun decides if the Rollout switches traffic, or abort the Rollout completely.

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: guestbook
spec:
...
  strategy:
    blueGreen:
      activeService: active-svc
      previewService: preview-svc
      prePromotionAnalysis:
        templates:
        - templateName: smoke-tests
        args:
        - name: service-name
          value: preview-svc.default.svc.cluster.local
```

In this example, the Rollout creates a pre-promotion AnalysisRun once the new ReplicaSet is fully available.
The Rollout will not switch traffic to the new version until the analysis run finishes successfully.

Note: if the`autoPromotionSeconds` field is specified and the Rollout has waited auto promotion seconds amount of time,
the Rollout marks the AnalysisRun successful and switches the traffic to a new version automatically. If the AnalysisRun
completes before then, the Rollout will not create another AnalysisRun and wait out the rest of the
`autoPromotionSeconds`.

## BlueGreen Post Promotion Analysis

A Rollout using a BlueGreen strategy can launch an analysis run *after* the traffic switch to the new version using
post-promotion analysis. If post-promotion Analysis fails or errors, the Rollout enters an aborted state and switches traffic back to the
previous stable Replicaset. When post-analysis is Successful, the Rollout is considered fully promoted and
the new ReplicaSet will be marked as stable. The old ReplicaSet will then be scaled down according to
`scaleDownDelaySeconds` (default 30 seconds).

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: guestbook
spec:
...
  strategy:
    blueGreen:
      activeService: active-svc
      previewService: preview-svc
      scaleDownDelaySeconds: 600 # 10 minutes
      postPromotionAnalysis:
        templates:
        - templateName: smoke-tests
        args:
        - name: service-name
          value: preview-svc.default.svc.cluster.local
```

## Failure Conditions and Failure Limit

`failureCondition` can be used to cause an analysis run to fail.
`failureLimit` is the maximum number of failed run an analysis is allowed.
The following example continually polls the defined Prometheus server to get the total number of errors(i.e., HTTP response code >= 500) every 5 minutes, causing the measurement to fail if ten or more errors are encountered.
The entire analysis run is considered as Failed after three failed measurements.

```yaml hl_lines="4 5"
  metrics:
  - name: total-errors
    interval: 5m
    failureCondition: result[0] >= 10
    failureLimit: 3
    provider:
      prometheus:
        address: http://prometheus.example.com:9090
        query: |
          sum(irate(
            istio_requests_total{reporter="source",destination_service=~"{{args.service-name}}",response_code=~"5.*"}[5m]
          ))
```

## Dry-Run Mode

!!! important
    Available since v1.2

`dryRun` can be used on a metric to control whether or not to evaluate that metric in a dry-run mode. A metric running 
in the dry-run mode won't impact the final state of the rollout or experiment even if it fails or the evaluation comes 
out as inconclusive.

The following example queries prometheus every 5 minutes to get the total number of 4XX and 5XX errors, and even if the
evaluation of the metric to monitor the 5XX error-rate fail, the analysis run will pass.

```yaml hl_lines="1 2"
  dryRun:
  - metricName: total-5xx-errors
  metrics:
  - name: total-5xx-errors
    interval: 5m
    failureCondition: result[0] >= 10
    failureLimit: 3
    provider:
      prometheus:
        address: http://prometheus.example.com:9090
        query: |
          sum(irate(
            istio_requests_total{reporter="source",destination_service=~"{{args.service-name}}",response_code~"5.*"}[5m]
          ))
  - name: total-4xx-errors
    interval: 5m
    failureCondition: result[0] >= 10
    failureLimit: 3
    provider:
      prometheus:
        address: http://prometheus.example.com:9090
        query: |
          sum(irate(
            istio_requests_total{reporter="source",destination_service=~"{{args.service-name}}",response_code~"4.*"}[5m]
          ))
```

RegEx matches are also supported. `.*` can be used to make all the metrics run in the dry-run mode. In the following 
example, even if one or both metrics fail, the analysis run will pass.

```yaml hl_lines="1 2"
  dryRun:
  - metricName: .*
  metrics:
  - name: total-5xx-errors
    interval: 5m
    failureCondition: result[0] >= 10
    failureLimit: 3
    provider:
      prometheus:
        address: http://prometheus.example.com:9090
        query: |
          sum(irate(
            istio_requests_total{reporter="source",destination_service=~"{{args.service-name}}",response_code~"5.*"}[5m]
          ))
  - name: total-4xx-errors
    interval: 5m
    failureCondition: result[0] >= 10
    failureLimit: 3
    provider:
      prometheus:
        address: http://prometheus.example.com:9090
        query: |
          sum(irate(
            istio_requests_total{reporter="source",destination_service=~"{{args.service-name}}",response_code~"4.*"}[5m]
          ))
```

### Dry-Run Summary

If one or more metrics are running in the dry-run mode, the summary of the dry-run results gets appended to the analysis 
run message. Assuming that the `total-4xx-errors` metric fails in the above example but, the `total-5xx-errors` 
succeeds, the final dry-run summary will look like this.

```yaml hl_lines="4 5 6 7"
Message: Run Terminated
Run Summary:
  ...
Dry Run Summary: 
  Count: 2
  Successful: 1
  Failed: 1
Metric Results:
...
```

### Dry-Run Rollouts

If a rollout wants to dry run its analysis, it simply needs to specify the `dryRun` field to its `analysis` stanza. In the 
following example, all the metrics from `random-fail` and `always-pass` get merged and executed in the dry-run mode.

```yaml hl_lines="9 10"
kind: Rollout
spec:
...
  steps:
  - analysis:
      templates:
      - templateName: random-fail
      - templateName: always-pass
      dryRun:
      - metricName: .*
```

### Dry-Run Experiments

If an experiment wants to dry run its analysis, it simply needs to specify the `dryRun` field under its specs. In the 
following example, all the metrics from `analyze-job` matching the RegEx rule `test.*` will be executed in the dry-run 
mode.

```yaml hl_lines="20 21"
kind: Experiment
spec:
  templates:
  - name: baseline
    selector:
      matchLabels:
        app: rollouts-demo
    template:
      metadata:
        labels:
          app: rollouts-demo
      spec:
        containers:
        - name: rollouts-demo
          image: argoproj/rollouts-demo:blue
  analyses:
  - name: analyze-job
    templateName: analyze-job
  dryRun:
  - metricName: test.*
```

## Measurements Retention

!!! important
    Available since v1.2

`measurementRetention` can be used to retain other than the latest ten results for the metrics running in any mode 
(dry/non-dry). Setting this option to `0` would disable it and, the controller will revert to the existing behavior of 
retaining the latest ten measurements.

The following example queries Prometheus every 5 minutes to get the total number of 4XX and 5XX errors and retains the 
latest twenty measurements for the 5XX metric run results instead of the default ten.

```yaml hl_lines="1 2 3"
  measurementRetention:
  - metricName: total-5xx-errors
    limit: 20
  metrics:
  - name: total-5xx-errors
    interval: 5m
    failureCondition: result[0] >= 10
    failureLimit: 3
    provider:
      prometheus:
        address: http://prometheus.example.com:9090
        query: |
          sum(irate(
            istio_requests_total{reporter="source",destination_service=~"{{args.service-name}}",response_code~"5.*"}[5m]
          ))
  - name: total-4xx-errors
    interval: 5m
    failureCondition: result[0] >= 10
    failureLimit: 3
    provider:
      prometheus:
        address: http://prometheus.example.com:9090
        query: |
          sum(irate(
            istio_requests_total{reporter="source",destination_service=~"{{args.service-name}}",response_code~"4.*"}[5m]
          ))
```

RegEx matches are also supported. `.*` can be used to apply the same retention rule to all the metrics. In the following 
example, the controller will retain the latest twenty run results for all the metrics instead of the default ten results.

```yaml hl_lines="1 2 3"
  measurementRetention:
  - metricName: .*
    limit: 20
  metrics:
  - name: total-5xx-errors
    interval: 5m
    failureCondition: result[0] >= 10
    failureLimit: 3
    provider:
      prometheus:
        address: http://prometheus.example.com:9090
        query: |
          sum(irate(
            istio_requests_total{reporter="source",destination_service=~"{{args.service-name}}",response_code~"5.*"}[5m]
          ))
  - name: total-4xx-errors
    interval: 5m
    failureCondition: result[0] >= 10
    failureLimit: 3
    provider:
      prometheus:
        address: http://prometheus.example.com:9090
        query: |
          sum(irate(
            istio_requests_total{reporter="source",destination_service=~"{{args.service-name}}",response_code~"4.*"}[5m]
          ))
```

### Measurements Retention for Rollouts Analysis

If a rollout wants to retain more results of its analysis metrics, it simply needs to specify the `measurementRetention` 
field to its `analysis` stanza. In the following example, all the metrics from `random-fail` and `always-pass` get 
merged, and their latest twenty measurements get retained instead of the default ten.

```yaml hl_lines="9 10 11"
kind: Rollout
spec:
...
  steps:
  - analysis:
      templates:
      - templateName: random-fail
      - templateName: always-pass
      measurementRetention:
      - metricName: .*
        limit: 20
```

### Define custom Labels/Annotations for AnalysisRun

If you would like to annotate/label the `AnalysisRun` with the custom labels your can do it by specifying 
`analysisRunMetadata` field.

```yaml hl_lines="9 10 11"
kind: Rollout
spec:
...
  steps:
  - analysis:
      templates:
      - templateName: my-template
      analysisRunMetadata:
        labels:
          my-custom-label: label-value
        annotations:
          my-custom-annotation: annotation-value
```

### Measurements Retention for Experiments

If an experiment wants to retain more results of its analysis metrics, it simply needs to specify the 
`measurementRetention` field under its specs. In the following example, all the metrics from `analyze-job` matching the 
RegEx rule `test.*` will have their latest twenty measurements get retained instead of the default ten.

```yaml hl_lines="20 21 22"
kind: Experiment
spec:
  templates:
  - name: baseline
    selector:
      matchLabels:
        app: rollouts-demo
    template:
      metadata:
        labels:
          app: rollouts-demo
      spec:
        containers:
        - name: rollouts-demo
          image: argoproj/rollouts-demo:blue
  analyses:
  - name: analyze-job
    templateName: analyze-job
  measurementRetention:
  - metricName: test.*
    limit: 20
```

## Inconclusive Runs

Analysis runs can also be considered `Inconclusive`, which indicates the run was neither successful,
nor failed. Inconclusive runs causes a rollout to become paused at its current step. Manual
intervention is then needed to either resume the rollout, or abort. One example of how analysis runs
could become `Inconclusive`, is when a metric defines no success or failure conditions.

```yaml
  metrics:
  - name: my-query
    provider:
      prometheus:
        address: http://prometheus.example.com:9090
        query: ...
```

`Inconclusive` analysis runs might also happen when both success and failure conditions are
specified, but the measurement value did not meet either condition.

```yaml
  metrics:
  - name: success-rate
    successCondition: result[0] >= 0.90
    failureCondition: result[0] < 0.50
    provider:
      prometheus:
        address: http://prometheus.example.com:9090
        query: ...
```

A use case for having `Inconclusive` analysis runs are to enable Argo Rollouts to automate the execution of analysis runs, and collect the measurement, but still allow human judgement to decide
whether or not measurement value is acceptable and decide to proceed or abort.

## Delay Analysis Runs
If the analysis run does not need to start immediately (i.e give the metric provider time to collect
metrics on the canary version), Analysis Runs can delay the specific metric analysis. Each metric
can be configured to have a different delay. In additional to the metric specific delays, the rollouts
with background analysis can delay creating an analysis run until a certain step is reached

Delaying a specific analysis metric:
```yaml hl_lines="3 4"
  metrics:
  - name: success-rate
    # Do not start this analysis until 5 minutes after the analysis run starts
    initialDelay: 5m
    successCondition: result[0] >= 0.90
    provider:
      prometheus:
        address: http://prometheus.example.com:9090
        query: ...
```

Delaying starting background analysis run until step 3 (Set Weight 40%):

```yaml hl_lines="11"
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: guestbook
spec:
  strategy:
    canary:
      analysis:
        templates:
        - templateName: success-rate
        startingStep: 2
      steps:
      - setWeight: 20
      - pause: {duration: 10m}
      - setWeight: 40
      - pause: {duration: 10m}
```
## Referencing Secrets

AnalysisTemplates and AnalysisRuns can reference secret objects in `.spec.args`. This allows users to securely pass authentication information to Metric Providers, like login credentials or API tokens.

An AnalysisRun can only reference secrets from the same namespace as it's running in. This is only relevant for AnalysisRuns, since AnalysisTemplates do not resolve the secret.

In the following example, an AnalysisTemplate references an API token and passes it to a Web metric provider.

This example demonstrates:

* The ability to reference a secret in the AnalysisTemplate `.spec.args`
* The ability to pass secret arguments to Metric Providers

```yaml
apiVersion: argoproj.io/v1alpha1
kind: AnalysisTemplate
spec:
  args:
  - name: api-token
    valueFrom:
      secretKeyRef:
        name: token-secret
        key: apiToken
  metrics:
  - name: webmetric
    provider:
      web:
        headers:
        - key: Authorization
          value: "Bearer {{ args.api-token }}"
```

## Handling Metric Results

### NaN and Infinity
Metric providers can sometimes return values of NaN (not a number) and infinity. Users can edit the `successCondition` and `failureCondition` fields
to handle these cases accordingly.

Here are three examples where a metric result of NaN is considered successful, inconclusive and failed respectively.

```yaml
apiVersion: argoproj.io/v1alpha1
kind: AnalysisRun
  ...
    successCondition: isNaN(result) || result >= 0.95
status:
  metricResults:
  - count: 1
    measurements:
    - finishedAt: "2021-02-10T00:15:26Z"
      phase: Successful
      startedAt: "2021-02-10T00:15:26Z"
      value: NaN
    name: success-rate
    phase: Successful
    successful: 1
  phase: Successful
  startedAt: "2021-02-10T00:15:26Z"
```

```yaml
apiVersion: argoproj.io/v1alpha1
kind: AnalysisRun
  ...
    successCondition: result >= 0.95
    failureCondition: result < 0.95
status:
  metricResults:
  - count: 1
    measurements:
    - finishedAt: "2021-02-10T00:15:26Z"
      phase: Inconclusive
      startedAt: "2021-02-10T00:15:26Z"
      value: NaN
    name: success-rate
    phase: Inconclusive
    successful: 1
  phase: Inconclusive
  startedAt: "2021-02-10T00:15:26Z"
```

```yaml
apiVersion: argoproj.io/v1alpha1
kind: AnalysisRun
  ...
    successCondition: result >= 0.95
status:
  metricResults:
  - count: 1
    measurements:
    - finishedAt: "2021-02-10T00:15:26Z"
      phase: Failed
      startedAt: "2021-02-10T00:15:26Z"
      value: NaN
    name: success-rate
    phase: Failed
    successful: 1
  phase: Failed
  startedAt: "2021-02-10T00:15:26Z"
```

Here are two examples where a metric result of infinity is considered successful and failed respectively.

```yaml
apiVersion: argoproj.io/v1alpha1
kind: AnalysisRun
  ...
    successCondition: result >= 0.95
status:
  metricResults:
  - count: 1
    measurements:
    - finishedAt: "2021-02-10T00:15:26Z"
      phase: Successful
      startedAt: "2021-02-10T00:15:26Z"
      value: +Inf
    name: success-rate
    phase: Successful
    successful: 1
  phase: Successful
  startedAt: "2021-02-10T00:15:26Z"
```

```yaml
apiVersion: argoproj.io/v1alpha1
kind: AnalysisRun
  ...
    failureCondition: isInf(result)
status:
  metricResults:
  - count: 1
    measurements:
    - finishedAt: "2021-02-10T00:15:26Z"
      phase: Failed
      startedAt: "2021-02-10T00:15:26Z"
      value: +Inf
    name: success-rate
    phase: Failed
    successful: 1
  phase: Failed
  startedAt: "2021-02-10T00:15:26Z"
```

### Empty array

#### Prometheus

Metric providers can sometimes return empty array, e.g., no data returned from prometheus query.

Here are two examples where a metric result of empty array is considered successful and failed respectively.

```yaml
apiVersion: argoproj.io/v1alpha1
kind: AnalysisRun
  ...
    successCondition: len(result) == 0 || result[0] >= 0.95
status:
  metricResults:
  - count: 1
    measurements:
    - finishedAt: "2021-09-08T19:15:49Z"
      phase: Successful
      startedAt: "2021-09-08T19:15:49Z"
      value: '[]'
    name: success-rate
    phase: Successful
    successful: 1
  phase: Successful
  startedAt:  "2021-09-08T19:15:49Z"
```

```yaml
apiVersion: argoproj.io/v1alpha1
kind: AnalysisRun
  ...
    successCondition: len(result) > 0 && result[0] >= 0.95
status:
  metricResults:
  - count: 1
    measurements:
    - finishedAt: "2021-09-08T19:19:44Z"
      phase: Failed
      startedAt: "2021-09-08T19:19:44Z"
      value: '[]'
    name: success-rate
    phase: Failed
    successful: 1
  phase: Failed
  startedAt: "2021-09-08T19:19:44Z"
```

#### Datadog

Datadog queries can return empty results if the query takes place during a time interval with no metrics. The Datadog provider will return a `nil` value yielding an error during the evaluation phase like:

```
invalid operation: < (mismatched types <nil> and float64)
```

However, empty query results yielding a `nil` value can be handled using the `default()` function. Here is a succeeding example using the `default()` function:

```yaml
successCondition: default(result, 0) < 0.05
```
