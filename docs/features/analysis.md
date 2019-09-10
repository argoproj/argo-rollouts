

# Canary Analysis Use Cases:

## Success Rate

Gradually increment canary weight by 20% every 10 minutes. In parallel, start an analysis run,
which measures success rates at 5 minute intervals/samples. Fail immediately and scale canary down
to zero when success rate is less than 95% three times. Rollout is considered `Degraded`

Demonstrates ability to parameterize the analysis.

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: rollouts-demo
spec:
...
  strategy:
    canary: 
      analysis:
        templateName: success-rate
        # NOTE: we may need a field to delay the analysis run until a specified step.
        # (e.g.: startingStepIndex: 1)
        arguments:
        - name: service-name
          value: my-canary.default.svc.local
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
  - name: sample-period
    default: '5m'
  metrics:
  - name: success-rate
    interval: 300 # 5 min
    successCondition: result >= 0.95
    maxFailures: 3
    prometheus:
      query: |
        sum(irate(
          istio_requests_total{reporter="source",destination_service=~"{{inputs.service-name}}",response_code!~"5.*"}[{{inputs.sample-period}}]
        )) / 
        sum(irate(
          istio_requests_total{reporter="source",destination_service=~"{{inputs.service-name}}"}[{{inputs.sample-period}}]
        ))
```


## Run analysis as a step

Increment weight, wait 5 minutes, then run analysis. Continue with rollout if analysis was
successful, otherwise set weight to zero if analysis failed.

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: rollouts-demo
  labels:
    app: rollouts-demo
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

The Analysis template is identical to above example, but because no interval is specified, will
perform a single measurement.

```yaml
apiVersion: argoproj.io/v1alpha1
kind: AnalysisTemplate
metadata:
  name: success-rate
spec:
  inputs:
  - name: service-name
  - name: sample-period
    default: '5m'
  metrics:
  - name: success-rate
    successCondition: result >= 0.95
    # NOTE: if the analysis should be run for a long duration, then the `count` and `interval` fields
    # can be used to measure the metric multiple times.
    # interval: 60
    # count: 5
    prometheus:
      query: |
        sum(irate(
          istio_requests_total{reporter="source",destination_service=~"{{inputs.service-name}}",response_code!~"5.*"}[{{inputs.sample-period}}]
        )) / 
        sum(irate(
          istio_requests_total{reporter="source",destination_service=~"{{inputs.service-name}}"}[{{inputs.sample-period}}]
        ))
```


## Failure Conditions

As an alternative to measuring success, failureCondition could be used to cause an analysis to
fail. The following example will cause analysis to fail if 10 or more errors were encountered.


```yaml
apiVersion: argoproj.io/v1alpha1
kind: AnalysisTemplate
metadata:
  name: total-failures
spec:
  inputs:
  - name: service-name
  metrics:
  - name: total-failures
    interval: 300
    failureCondition: result >= 10
    maxFailures: 3
    prometheus:
      query: |
        sum(irate(
          istio_requests_total{reporter="source",destination_service=~"{{inputs.service-name}}",response_code~"5.*"}[5m]
        ))
```


## Mann Whitney Experiment

Start both a canary and baseline pod. Run experiment for 1 hour, then scale down the pods. Call out
to Kayenta to perform Mann Whintney analysis against the two pods. Demonstrates ability to start a
short-lived experiment and an asynchronous analysis.

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
      url: https://kayenta.intuit.com
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

The above would intantiate the following experiment

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Experiment
name:
  name: guestbook-6c54544bf9-0
spec:
  durationSeconds: 3600
  templates:
  - name: canary
    replicas: 1
    spec:
      containers:
      - name: guestbook
        image: guesbook:v2
  - name: baseline
    replicas: 1
    spec:
      containers:
      - name: guestbook
        image: guesbook:v1
  analysis:
    templateName: mann-whitney
    arguments:
    - name: start-time
      value: 2019-08-
    - name: end-time
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
      url: https://kayenta.intuit.com
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

Indefinite experiments can be run by omitting the duration field

## Blue-Green Automated Rollback

Perform a blue-green deployment. After the cutover, run analysis.

