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
    * Two arguments with the same name both have values

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
  # required
  - name: service-name
  - name: stable-hash
  - name: latest-hash
  # optional
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
        # paceholders are resolved when an AnalysisRun is created 
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

## BlueGreen Pre Promotion Analysis
A Rollout using the BlueGreen strategy can launch an AnalysisRun before it switches traffic to the new version. The
AnalysisRun can be used to block the Service selector switch until the AnalysisRun finishes successful. The success or
failure of the analysis run decides if the Rollout will switch traffic, or abort the Rollout completely.

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

In this example, the Rollout is creating a AnalysisRun once the new version has all the pods available. The 
Rollout will not switch traffic to the new version until the analysis run finishes successfully. 

Note: if the`autoPromotionSeconds` field is specified and the Rollout has waited auto promotion seconds amount of time,
the Rollout marks the AnalysisRun successful and switches the traffic to a new version automatically. If the AnalysisRun
completes before then, the Rollout will not create another AnalysisRun and wait out the rest of the 
`autoPromotionSeconds`.

## BlueGreen Post Promotion Analysis

A Rollout using a BlueGreen strategy can launch an analysis run after the traffic switch to new version. If the analysis
run fails or errors out, the Rollout enters an aborted state and switch traffic back to the previous stable Replicaset.
If `scaleDownDelaySeconds` is specified, the controller will cancel any AnalysisRuns at time of `scaleDownDelay` to 
scale down the ReplicaSet. If it is omitted, and post analysis is specified, it will scale down the ReplicaSet only 
after the AnalysisRun completes (with a minimum of 30 seconds).

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

## Failure Conditions

`failureCondition` can be used to cause an analysis run to fail. The following example continually polls a prometheus 
server to get the total number of errors every 5 minutes, causing the analysis run to fail if 10 or more errors were 
encountered.

```yaml hl_lines="4"
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
            istio_requests_total{reporter="source",destination_service=~"{{args.service-name}}",response_code~"5.*"}[5m]
          ))
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

## Experimentation (e.g. Mann-Whitney Analysis)

Analysis can also be done as part of an Experiment. 

This example starts both a canary and baseline ReplicaSet. The ReplicaSets run for 1 hour, then
scale down to zero. Call out to Kayenta to perform Mann-Whintney analysis against the two pods. Demonstrates ability to start a
short-lived experiment and an asynchronous analysis.

This example demonstrates:

* The ability to start an [Experiment](experiment.md) as part of rollout steps, which launches multiple ReplicaSets (e.g. baseline & canary)
* The ability to reference and supply pod-template-hash to an AnalysisRun
* Kayenta metrics

=== "Rollout"

    ```yaml
    apiVersion: argoproj.io/v1alpha1
    kind: Rollout
    metadata:
      name: guestbook
      labels:
        app: guestbook
    spec:
      strategy:
        canary: 
          steps:
          - experiment:
              duration: 1h
              templates:
              - name: baseline
                specRef: stable
              - name: canary
                specRef: canary
              analysis:
                templateName: mann-whitney
                args:
                - name: stable-hash
                  valueFrom:
                    podTemplateHashValue: Stable
                - name: canary-hash
                  valueFrom:
                    podTemplateHashValue: Latest
    ```

=== "AnalysisTemplate"

    ```yaml 
    apiVersion: argoproj.io/v1alpha1
    kind: AnalysisTemplate
    metadata:
      name: mann-whitney
    spec:
      args:
      - name: start-time
      - name: end-time
      - name: stable-hash
      - name: canary-hash
      metrics:
      - name: mann-whitney
        provider:
          kayenta:
            address: https://kayenta.example.com
            application: guestbook
            canaryConfigName: my-test
            thresholds:
              pass: 90
              marginal: 75
            scopes:
            - name: default
              controlScope:
                scope: app=guestbook and rollouts-pod-template-hash={{args.stable-hash}}
                step: 60
                start: "{{args.start-time}}"
                end: "{{args.end-time}}"
              experimentScope:
                scope: app=guestbook and rollouts-pod-template-hash={{args.canary-hash}}
                step: 60
                start: "{{args.start-time}}"
                end: "{{args.end-time}}"
    ```

=== "Experiment"

    ```yaml
    apiVersion: argoproj.io/v1alpha1
    kind: Experiment
    name:
      name: guestbook-6c54544bf9-0
    spec:
      duration: 1h
      templates:
      - name: baseline
        replicas: 1
        spec:
          containers:
          - name: guestbook
            image: guestbook:v1
      - name: canary
        replicas: 1
        spec:
          containers:
          - name: guestbook
            image: guestbook:v2
      analysis:
        templateName: mann-whitney
        args:
        - name: start-time
          value: "{{experiment.availableAt}}"
        - name: end-time
          value: "{{experiment.finishedAt}}"
    ```


In order to perform multiple kayenta runs over some time duration, the `interval` and `count` fields
can be supplied. When the `start` and `end` fields are omitted from the kayenta scopes, the values
will be implicitly decided as:

* `start` = if `lookback: true` start of analysis, otherwise current time - interval
* `end` = current time


```yaml
apiVersion: argoproj.io/v1alpha1
kind: AnalysisTemplate
metadata:
  name: mann-whitney
spec:
  args:
  - name: stable-hash
  - name: canary-hash
  metrics:
  - name: mann-whitney
    provider:
      kayenta:
        address: https://kayenta.intuit.com
        application: guestbook
        canaryConfigName: my-test
        interval: 3600
        count: 3
        # loopback will cause start time value to be equal to start of analysis
        # lookback: true
        thresholds:
          pass: 90
          marginal: 75
        scopes:
        - name: default
          controlScope:
            scope: app=guestbook and rollouts-pod-template-hash={{args.stable-hash}}
            step: 60
          experimentScope:
            scope: app=guestbook and rollouts-pod-template-hash={{args.canary-hash}}
            step: 60
```

## Run Experiment Indefinitely

Experiments can run for an indefinite duration by omitting the duration field. Indefinite
experiments would be stopped externally, or through the completion of a referenced analysis.


## Job Metrics

A Kubernetes Job can be used to run analysis. When a Job is used, the metric is considered
successful if the Job completes and had an exit code of zero, otherwise it is failed.

```yaml
  metrics:
  - name: test
    provider:
      job:
        spec:
          template:
            backoffLimit: 1
            spec:
              containers:
              - name: test
                image: my-image:latest
                command: [my-test-script, my-service.default.svc.cluster.local]
              restartPolicy: Never
```

## Wavefront Metrics

A [Wavefront](https://www.wavefront.com/) query can be used to obtain measurements for analysis.

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
    successCondition: result >= 0.95
    failureLimit: 3
    provider:
      wavefront:
        address: example.wavefront.com
        query: |
          sum(rate(
            5m, ts("istio.requestcount.count", response_code!=500 and destination_service="{{args.service-name}}"
          ))) /
          sum(rate(
            5m, ts("istio.requestcount.count", reporter=client and destination_service="{{args.service-name}}"
          )))
```

Wavefront api tokens can be configured in a kubernetes secret in argo-rollouts namespace.

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: wavefront-api-tokens
type: Opaque
data:
  example1.wavefront.com: <token1>
  example2.wavefront.com: <token2>
```

## Web Metrics

A webhook can be used to call out to some external service to obtain the measurement. This example makes a HTTP GET request to some URL. The webhook response must return JSON content. The result of the `jsonPath` expression will be assigned to the `result` variable that can be referenced in the `successCondition` and `failureCondition` expressions.

```yaml
  metrics:
  - name: webmetric
    successCondition: result == 'true'
    provider:
      web:
        url: "http://my-server.com/api/v1/measurement?service={{ args.service-name }}"
        timeoutSeconds: 20 # defaults to 10 seconds
        headers:
          - key: Authorization
            value: "Bearer {{ args.api-token }}"
        jsonPath: "{$.results.ok}" 
```

In this example, the measurement is successful if the json response returns `"true"` for the nested `ok` field.  

```json
{ "results": { "ok": "true", "successPercent": 0.95 } }
```

For success conditions that need to evaluate a numeric return value the `asInt` or `asFloat` functions can be used to convert the result value.

```yaml
  metrics:
  - name: webmetric
    successCondition: "asFloat(result) >= 0.90"
    provider:
      web:
        url: "http://my-server.com/api/v1/measurement?service={{ args.service-name }}"
        headers:
          - key: Authorization
            value: "Bearer {{ args.api-token }}"
        jsonPath: "{$.results.successPercent}" 
```
