# Canary Analysis & Progressive Delivery

> NOTE: this is the API spec for Rollouts (v0.6) to perform canary analysis and progressive
delivery during a rollout and is subject to change

Argo Rollouts provides several ways to perform canary analysis to drive progressive delivery.
This document describes how to achieve various forms of progressive delivery, varying the point in
time analysis is performed, it's frequency, and occurrence.

# Custom Resource Definitions

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
        templateName: success-rate
        # NOTE: a field may be introduced to delay starting analysis run until a specified step is reached.
        # (e.g.: startingStepIndex: 1)
        arguments:
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


```yaml
apiVersion: argoproj.io/v1alpha1
kind: AnalysisTemplate
metadata:
  name: success-rate
spec:
  inputs:
  - name: service-name
  metrics:
  - name: success-rate
    interval: 300 # 5 min
    successCondition: result >= 0.95
    maxFailures: 3
    prometheus:
      address: http://prometheus.example.com:9090
      query: |
        sum(irate(
          istio_requests_total{reporter="source",destination_service=~"{{inputs.service-name}}",response_code!~"5.*"}[5m]
        )) / 
        sum(irate(
          istio_requests_total{reporter="source",destination_service=~"{{inputs.service-name}}"}[5m]
        ))
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
          templateName: success-rate
```

In this example, the `AnalysisTemplate` is identical to the background analysis example, but since
no interval is specified, the analysis will perform a single measurement and complete.

```yaml
apiVersion: argoproj.io/v1alpha1
kind: AnalysisTemplate
metadata:
  name: success-rate
spec:
  inputs:
  - name: service-name
  metrics:
  - name: success-rate
    successCondition: result >= 0.95
    prometheus:
      address: http://prometheus.example.com:9090
      query: |
        sum(irate(
          istio_requests_total{reporter="source",destination_service=~"{{inputs.service-name}}",response_code!~"5.*"}[5m]
        )) / 
        sum(irate(
          istio_requests_total{reporter="source",destination_service=~"{{inputs.service-name}}"}[5m]
        ))
```

Multiple measurements can be performed over a longer duration period, by specifying the `count` and 
`interval` fields:

```yaml
  metrics:
  - name: success-rate
    successCondition: result >= 0.95
    interval: 60
    count: 5
    prometheus:
      address: http://prometheus.example.com:9090
      query: ...
```

## Failure Conditions

As an alternative to measuring success, `failureCondition` can be used to cause an analysis run to
fail. The following example continually polls a prometheus server to get the total number of errors
every 5 minutes, causing the analysis run to fail if 10 or more errors were encountered.

```yaml
  metrics:
  - name: total-errors
    interval: 300
    failureCondition: result >= 10
    maxFailures: 3
    prometheus:
      address: http://prometheus.example.com:9090
      query: |
        sum(irate(
          istio_requests_total{reporter="source",destination_service=~"{{inputs.service-name}}",response_code~"5.*"}[5m]
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
    prometheus:
      address: http://prometheus.example.com:9090
      query: ...
```

A use case for having `Inconclusive` analysis runs are to enable Argo Rollouts to automate the execution of analysis runs, and collect the measurement, but still allow human judgement to decide
whether or not measurement value is acceptable and decide to proceed or abort.

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
            arguments:
            - name: stable-hash
              valueFrom:
                podTemplateHash: baseline
            - name: canary-hash
              valueFrom:
                podTemplateHash: canary
```


```yaml
apiVersion: argoproj.io/v1alpha1
kind: AnalysisTemplate
metadata:
  name: mann-whitney
spec:
  inputs:
  - name: start-time
  - name: end-time
  - name: stable-hash
  - name: canary-hash
  metrics:
  - name: mann-whitney
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
          scope: app=guestbook and rollouts-pod-template-hash={{inputs.stable-hash}}
          step: 60
          start: "{{inputs.start-time}}"
          end: "{{inputs.end-time}}"
        experimentScope:
          scope: app=guestbook and rollouts-pod-template-hash={{inputs.canary-hash}}
          step: 60
          start: "{{inputs.start-time}}"
          end: "{{inputs.end-time}}"
```

The above would instantiate the following experiment:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Experiment
name:
  name: guestbook-6c54544bf9-0
spec:
  durationSeconds: 3600
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
    arguments:
    - name: start-time
      value: 2019-09-14T01:40:10Z
    - name: end-time
      value: 2019-09-14T02:40:10Z
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
  inputs:
  - name: stable-hash
  - name: canary-hash
  metrics:
  - name: mann-whitney
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
          scope: app=guestbook and rollouts-pod-template-hash={{inputs.stable-hash}}
          step: 60
        experimentScope:
          scope: app=guestbook and rollouts-pod-template-hash={{inputs.canary-hash}}
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
    job:
      backoffLimit: 1
      template:
        spec:
          containers:
          - name: test
            image: my-image:latest
            command: [my-test-script, my-service.default.svc.cluster.local]
          restartPolicy: Never
```

## Webhook Metrics

Aside from the built-in metric types such as prometheus, kayenta, A webhook can be used to call out to some external service to obtain the measurement. This example
makes a HTTP request to some URL. The webhook response should return a JSON return value. In this
example, the measurement is successful if the result's `my-metric` field was in the set [A, B, C]. 

```yaml
  metrics:
  - name: webhook
    successCondition: result.my-metric in (A, B, C)
    webhook:
      url: http://my-server.com/api/v1/measurement
```


