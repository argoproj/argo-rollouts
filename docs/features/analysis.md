# Canary Analysis & Progressive Delivery

Argo Rollouts provides several ways to perform canary analysis to drive progressive delivery.
This document describes how to achieve various forms of progressive delivery, varying the point in
time analysis is performed, it's frequency, and occurrence.

## Custom Resource Definitions

| CRD                 | Description |
|---------------------|-------------|
| Rollout             | A `Rollout` acts as a drop-in replacement for a Deployment resource. It provides additional blueGreen and canary update strategies. These strategies can create AnalysisRuns and Experiments during the update, which will progress the update, or abort it. |
| AnalysisTemplate    | An `AnalysisTemplate` is a template spec which defines *_how_* to perform a canary analysis, such as the metrics which it should perform, its frequency, and the values which are considered successful or failed. AnalysisTemplates may be parameterized with inputs values. |
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

* background analysis style of progressive delivery
* using a prometheus query to perform a measurement
* the ability to parameterize the analysis
* Delay starting the analysis run until step 3 (Set Weight 40%)

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
        startingStep: 2 # delay starting analysis run
                        # until setWeight: 40%
        args:
        - name: service-name
          value: guestbook-svc.default.svc.cluster.local
      steps:
      - setWeight: 20
      - pause: {duration: 600}
      - setWeight: 40
      - pause: {duration: 600}
      - setWeight: 60
      - pause: {duration: 600}
      - setWeight: 80
      - pause: {duration: 600}
```

Note: Previously, the analysis section had a field called "templateName" where a user would specify a single
AnalysisTemplate. This field has be depreciated in lieu of the templates field, and the field will be removed in v0.9.0. 

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

* analysis using wavefront query

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

wavefront api tokens can be configured in a kubernetes secret in argo-rollouts namespace.

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

## Analysis at a Predefined Step

Analysis can also be performed as a rollout step as a "analysis" step. When analysis is performed
as a step, an `AnalysisRun` is started when the step is reached, and blocks the rollout until the
run is completed. The success or failure of the analysis run decides if the rollout will proceed to
the next step, or abort the rollout completely.

This example sets the canary weight to 20%, pauses for 5 minutes, then runs an analysis. If the
analysis was successful, continues with rollout, otherwise aborts.

This example demonstrates:

* the ability to invoke an analysis in-line as part of steps

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
      - pause: {duration: 300}
      - analysis:
          templates:
          - templateName: success-rate
          args:
          - name: service-name
            value: guestbook-svc.default.svc.cluster.local
```

Note: Previously, the analysis section had a field called "templateName" where a user would specify a single
AnalysisTemplate. This field has be depreciated in lieu of the templates field. and the field will be removed in v0.9.0. 

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
  metrics:
  - name: success-rate
    successCondition: result >= 0.95
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

Multiple measurements can be performed over a longer duration period, by specifying the `count` and 
`interval` fields:

```yaml
  metrics:
  - name: success-rate
    successCondition: result >= 0.95
    interval: 60s
    count: 5
    provider:
      prometheus:
        address: http://prometheus.example.com:9090
        query: ...
```

## Analysis with multiple templates
A Rollout can reference multiple AnalysisTemplates when constructing an AnalysisRun. This allows users to compose 
analysis from multiple AnalysisTemplates. If multiple templates are referenced, then the controller will merge the
templates together. The controller combines the metrics and args fields of all the templates.


Rollout referencing multiple AnalysisTemplates:
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
The AnalysisTemplates:
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
    successCondition: result <= 0.95
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

The controller would create an AnalysisRun with the following yaml:
```yaml
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
    successCondition: result >= 0.95
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
    successCondition: result <= 0.95
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

Note: The controller will error when merging the templates if:
* multiple metrics in the templates have the same name
* Two arguments with the same name both have values

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
If the `scaleDownDelaySeconds` has passed for the previous ReplicaSet, the AnalysisRun for that ReplicaSet is marked as 
successful. If the AnalysisRun completes before the `scaleDownDelaySeconds`, the Rollout will not create another 
AnalysisRun and wait out the rest of `scaleDownDelaySeconds` before scaling down the previous ReplicaSet.

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

```yaml
  metrics:
  - name: total-errors
    interval: 5m
    failureCondition: result >= 10
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
    successCondition: result >= 0.90
    failureCondition: result < 0.50
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
```yaml
  metrics:
  - name: success-rate
    initialDelay: 5m # Do not start this analysis until 5 minutes after the analysis run starts
    successCondition: result >= 0.90
    provider:
      prometheus:
        address: http://prometheus.example.com:9090
        query: ...
```

Delaying starting background analysis run until step 3 (Set Weight 40%):

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
        startingStep: 2
      steps:
      - setWeight: 20
      - pause: {duration: 600}
      - setWeight: 40
      - pause: {duration: 600}
```

## Experimentation (e.g. Mann-Whitney Analysis)

Analysis can also be done as part of an Experiment. 

This example starts both a canary and baseline ReplicaSet. The ReplicaSets run for 1 hour, then
scale down to zero. Call out to Kayenta to perform Mann-Whintney analysis against the two pods. Demonstrates ability to start a
short-lived experiment and an asynchronous analysis.

This example demonstrates:

* the ability to start an Experiment as part of rollout steps, which launches multiple ReplicaSets (e.g. baseline & canary)
* the ability to reference and supply pod-template-hash to an AnalysisRun
* kayenta metrics

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: guestbook
  labels:
    app: guestbook
spec:
...
  strategy:
    canary: 
      steps:
      - experiment:
          duration: 3600
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

The above would instantiate the following experiment:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Experiment
name:
  name: guestbook-6c54544bf9-0
spec:
  duration: 3600
  templates:
  - name: baseline
    replicas: 1
    spec:
      containers:
      - name: guestbook
        image: guesbook:v1
  - name: canary
    replicas: 1
    spec:
      containers:
      - name: guestbook
        image: guesbook:v2
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

* start: if `lookback: true` start of analysis, otherwise current time - interval
* end: current time


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

## Run experiment indefinitely

Experiments can run for an indefinite duration by omitting the duration field. Indefinite
experiments would be stopped externally, or through the completion of a referenced analysis.

## Blue-Green Automated Rollback

Perform a blue-green deployment. After the cutover, run analysis. If the analysis succeeds, the rollout is successful, otherwise abort the rollout and cut traffic back over to the stable replicaset.

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: guestbook
spec:
...
  strategy:
    blueGreen: 
      postAnalysis:
        templateName: success-rate
```


## Job Metrics

A Kubernetes Job can be used to run analysis. When a Job is used, the metric is considered
successful if the Job completes and had an exit code of zero, otherwise it is failed.

```yaml
  metrics:
  - name: test
    provider:
      job:
        backoffLimit: 1
        spec:
          template:
            spec:
              containers:
              - name: test
                image: my-image:latest
                command: [my-test-script, my-service.default.svc.cluster.local]
              restartPolicy: Never
```

## Web Metrics

A webhook can be used to call out to some external service to obtain the measurement. This example makes a HTTP GET request to some URL. The webhook response should return JSON content. 

```yaml
  metrics:
  - name: webmetric
    successCondition: "true"
    provider:
      web:
        url: "http://my-server.com/api/v1/measurement?service={{ args.service-name }}"
        timeoutSeconds: 20 # defaults to 10 seconds
        headers:
          - key: X-Measurement-Token
            value: "{{ args.token }}"
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
          - key: X-Measurement-Token
            value: "{{ args.token }}"
        jsonPath: "{$.results.successPercent}" 
```




