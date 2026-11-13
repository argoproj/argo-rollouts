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
            metricsAccountName: my-metrics-account
            configurationAccountName: my-kayenta-account
            storageAccountName: my-kayenta-account
            threshold:
              pass: 90
              marginal: 75
            scopes:
            - name: default
              controlScope:
                scope: app=guestbook and rollouts-pod-template-hash={{args.stable-hash}}
                region: us-west-2
                step: 60
                start: "{{args.start-time}}"
                end: "{{args.end-time}}"
              experimentScope:
                scope: app=guestbook and rollouts-pod-template-hash={{args.canary-hash}}
                region: us-west-2
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


In order to perform multiple kayenta runs over some time duration, the metric-level `interval` and `count`
fields can be supplied. When the `start` and `end` fields are omitted from the kayenta scopes, the values
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
    interval: 1h
    count: 3
    provider:
      kayenta:
        address: https://kayenta.intuit.com
        application: guestbook
        canaryConfigName: my-test
        metricsAccountName: my-metrics-account
        configurationAccountName: my-kayenta-account
        storageAccountName: my-kayenta-account
        # lookback will cause start time value to be equal to start of analysis
        # lookback: true
        threshold:
          pass: 90
          marginal: 75
        scopes:
        - name: default
          controlScope:
            scope: app=guestbook and rollouts-pod-template-hash={{args.stable-hash}}
            region: us-west-2
            step: 60
          experimentScope:
            scope: app=guestbook and rollouts-pod-template-hash={{args.canary-hash}}
            region: us-west-2
            step: 60
```

## Fields

| Field | Required | Description |
|-------|----------|-------------|
| `address` | yes | Base URL of the Kayenta server. |
| `application` | yes | Spinnaker application the canary config belongs to. |
| `canaryConfigName` | yes | Name of the canary config to run; the controller looks up its id by application and name. |
| `metricsAccountName` | yes | Kayenta account used to fetch the metrics. |
| `configurationAccountName` | yes | Kayenta account passed as the configuration account when the canary is started. |
| `storageAccountName` | yes | Kayenta account that stores the canary results. The controller also uses this account, not `configurationAccountName`, when it looks up the canary config id by name, so the canary config must be readable through it. Using one account for both avoids surprises. |
| `threshold.pass` | yes | Score at or above which the measurement is `Successful`. |
| `threshold.marginal` | yes | Score at or above which the measurement is `Inconclusive` instead of `Failed`. |
| `scopes[].name` | yes | Scope name as defined in the canary config. |
| `scopes[].controlScope`, `scopes[].experimentScope` | yes | Baseline and canary scopes. Each needs `scope`, `region` and `step`; `start` and `end` are optional (see below). |
| `lookback` | no | When `true`, an omitted `start` is the start of the analysis rather than the current time minus `interval`. |
