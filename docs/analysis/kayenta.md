## Kayenta (e.g. Mann-Whitney Analysis)

Analysis can also be done as part of an [Experiment](../features/experiment.md).

This example starts both a canary and baseline ReplicaSet. The ReplicaSets run for 1 hour, then
scale down to zero. Call out to Kayenta to perform Mann-Whintney analysis against the two pods. Demonstrates ability to start a
short-lived experiment and an asynchronous analysis.

This example demonstrates:

* The ability to start an [Experiment](../features/experiment.md) as part of rollout steps, which launches multiple ReplicaSets (e.g. baseline & canary)
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
              analyses:
              - templateName: mann-whitney
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
    # This is the resulting experiment that is produced by the step
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
