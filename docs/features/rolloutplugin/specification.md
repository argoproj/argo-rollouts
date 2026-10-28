# RolloutPlugin Specification

!!! warning "Alpha Feature"

    The `RolloutPlugin` API is alpha and may change in future releases.

```yaml
apiVersion: argoproj.io/v1alpha1
kind: RolloutPlugin
metadata:
  name: example-rolloutplugin
spec:
  # Reference to the workload to roll out, in the same namespace as the RolloutPlugin.
  # The kind must be the one the plugin manages. Required.
  workloadRef:
    apiVersion: apps/v1
    kind: StatefulSet
    name: my-app

  # Resource plugin that drives the workload. The name must match an entry under
  # resourcePlugins in the argo-rollouts-config ConfigMap. Required.
  plugin:
    name: argoproj/statefulset
    # Optional, plugin-specific configuration, passed to the plugin as is.
    # The built-in StatefulSet plugin accepts none.
    config: {}

  # Pauses the rollout at its current step. Defaults to false.
  paused: false

  # Seconds the rollout can go without making progress before it is marked
  # as timed out. Paused time doesn't count. Defaults to 600, must be greater
  # than 0.
  timeoutSeconds: 600

  # Abort the rollout when it times out. Defaults to false.
  timeoutAbort: false

  # Number of successful and unsuccessful AnalysisRuns to keep. Defaults to 5 each.
  analysis:
    successfulRunHistoryLimit: 5
    unsuccessfulRunHistoryLimit: 5

  strategy:
    # The only supported strategy. Required.
    canary:
      # Background analysis, run for the whole rollout. Optional.
      analysis:
        templates:
        - templateName: error-rate
        # Start the background analysis at this step index instead of step 0.
        startingStep: 1
        args:
        - name: service-name
          value: my-app
        # Metrics whose failures don't abort the rollout
        dryRun:
        - metricName: error-rate
        # Number of measurements to keep per metric
        measurementRetention:
        - metricName: error-rate
          limit: 20
        # Labels and annotations added to the AnalysisRun
        analysisRunMetadata:
          labels:
            team: my-team

      # Steps executed in order. Each step has exactly one of
      # setWeight, pause or analysis. Optional: with no steps, the
      # new revision is promoted to every pod straight away.
      steps:
      # Move 20% of the pods (rounded up) to the new revision
      - setWeight: 20

      # Pause until the rollout is promoted
      - pause: {}

      # Run an AnalysisRun and wait for its result
      - analysis:
          templates:
          - templateName: smoke-test
          - templateName: cluster-wide-check
            clusterScope: true
          args:
          - name: rolloutplugin-name
            valueFrom:
              fieldRef:
                fieldPath: metadata.name

      - setWeight: 60

      # Pause for a duration (s, m or h suffix, or seconds as an integer)
      - pause:
          duration: 5m

      - setWeight: 100

status:
  # Set by controller or Argo CD actions on the status subresource,
  # see Actions and Lifecycle.
  abort: false
  promoteFull: false
  restart: false
  pauseConditions: []
```

## Spec fields

### workloadRef

Identifies the workload, by `apiVersion`, `kind` and `name`, in the RolloutPlugin's namespace. All three fields are
required. The plugin must manage that kind, otherwise the spec is invalid. While the workload doesn't exist, the
RolloutPlugin reports an error and retries.

### plugin

| Field    | Description                                                                                                                      |
| -------- | -------------------------------------------------------------------------------------------------------------------------------- |
| `name`   | Required. Name of a plugin enabled under `resourcePlugins` in the `argo-rollouts-config` ConfigMap, e.g. `argoproj/statefulset`. |
| `config` | Optional. Plugin-specific settings, passed to the plugin as is; see the plugin's documentation. The StatefulSet plugin has none. |

### paused

`true` holds the rollout at its current step: no weight is applied and no step completes, and the paused time doesn't
count toward `timeoutSeconds`. A pause step's duration still counts from when the step started, so a duration that
elapsed while paused completes the step on resume. Analysis keeps running, and a failed analysis or an abort is still
acted on. Promote full clears it.

### timeoutSeconds and timeoutAbort

`timeoutSeconds` is how long a rollout can go without progress. Progress is any of: a step completes,
the rollout resumes, the number of updated pods changes, fewer pods remain on the old revision, more pods become
ready or available, or the stable revision changes. Paused time doesn't count.

When the timeout is exceeded, the rollout is marked as timed out (`ProgressDeadlineExceeded`), the phase becomes
`Degraded`, and the running analysis is stopped. The rollout holds where it is until a new revision is deployed, or
it is promoted full, paused and resumed, or aborted and restarted. With `timeoutAbort: true` the rollout is aborted
instead, also when `timeoutAbort` is set after the timeout.

### analysis

`successfulRunHistoryLimit` and `unsuccessfulRunHistoryLimit` limit the AnalysisRuns kept from
earlier revisions. AnalysisRuns of the current and updated revisions are always kept.

### strategy.canary.steps

Each step has exactly one of the following fields.

| Step        | Behavior                                                                                                                                                                                                                                                                              |
| ----------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `setWeight` | Percentage of pods (0–100) to move to the new revision. The step completes once the plugin verifies the weight, for example once the pods are updated and available.                                                                                                                  |
| `pause`     | Holds the rollout. Without a `duration` it waits until promoted. With a `duration` it continues once the duration elapses. The weight of the last `setWeight` step before it (0 if none) is kept, so a scale-up during the pause is brought to that weight too.                       |
| `analysis`  | Creates an AnalysisRun and waits for it. `Successful` advances, `Failed` or `Error` aborts the rollout, and `Inconclusive` pauses it; promoting then runs the analysis again. Like a pause, the weight of the last `setWeight` step is kept. Every metric must have a finite `count`. |

Editing the steps while a rollout is in progress restarts it from the first step.

### strategy.canary.analysis

A background analysis is created when the rollout reaches `startingStep` and runs until the rollout
completes, is aborted, or is promoted full. A failed or errored run aborts the rollout; an inconclusive run pauses it,
and promoting starts a new run. Unlike step analysis, a background metric may run indefinitely (no `count`).

### Analysis fields

Step and background analysis share these fields, with the same meaning as in a [Rollout](../analysis.md):

| Field                  | Description                                                                                                                                                                                                                                                                                             |
| ---------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `templates`            | `AnalysisTemplate`s by `templateName`, or `ClusterAnalysisTemplate`s with `clusterScope: true`. The templates must exist.                                                                                                                                                                               |
| `args`                 | Arguments for the templates. `valueFrom.podTemplateHashValue` resolves `Stable` to the stable revision and `Latest` to the revision being rolled out (for StatefulSets, the value of the pods' `controller-revision-hash` label). `valueFrom.fieldRef.fieldPath` resolves a field of the RolloutPlugin. |
| `dryRun`               | Metrics whose results don't affect the rollout.                                                                                                                                                                                                                                                         |
| `measurementRetention` | Number of measurements to keep per metric.                                                                                                                                                                                                                                                              |
| `analysisRunMetadata`  | Labels and annotations added to the AnalysisRun. They can't override the labels the controller sets.                                                                                                                                                                                                    |

## Validation

The spec is validated continuously. An invalid spec sets the phase to `Degraded`, with the reason in the status
message, and the workload isn't touched until the spec is fixed:

* `workloadRef.apiVersion`, `workloadRef.kind`, `workloadRef.name`, `plugin.name` and `strategy.canary` are set.
* `timeoutSeconds` is greater than 0.
* Each step has exactly one of `setWeight`, `pause` or `analysis`; `setWeight` is between 0 and 100; `pause.duration` isn't negative.
* Referenced analysis templates exist, their arguments resolve, metric names are unique across the templates of an
  analysis, and step analysis metrics have a finite `count`.
* The plugin is enabled in the `argo-rollouts-config` ConfigMap.
* The plugin accepts the spec, including the workload kind and `plugin.config`.

## Status

The phase is shown by `kubectl get rolloutplugin`; see [Checking the status](index.md#checking-the-status). The main
status fields are:

| Field                                                               | Description                                                                                      |
| ------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------ |
| `phase`, `message`                                                  | `Healthy`, `Progressing`, `Paused` or `Degraded`, and a human-readable message.                  |
| `currentRevision`                                                   | The stable revision.                                                                             |
| `updatedRevision`                                                   | The revision being rolled out. A rollout is in progress while it differs from `currentRevision`. |
| `currentStepIndex`                                                  | Index of the current step.                                                                       |
| `replicas`, `updatedReplicas`, `readyReplicas`, `availableReplicas` | Replica counts of the workload.                                                                  |
| `pauseConditions`                                                   | Why the rollout is paused. Clear it to [promote](actions.md#promote).                            |
| `abort`, `promoteFull`, `restart`                                   | Requests set by users; see [Actions and Lifecycle](actions.md).                                  |
| `aborted`, `abortedAt`                                              | Set once an abort has rolled every pod back.                                                     |
| `restartCount`                                                      | Number of restarts of the current revision.                                                      |

The RolloutPlugin also records Kubernetes events, for example when a rollout starts, pauses, completes or is aborted.
They can be sent as [notifications](../notifications.md#rolloutplugin).

## AnalysisRuns

AnalysisRuns are owned by the RolloutPlugin and deleted with it. They are named after the RolloutPlugin, the revision
and, for step analysis, the step index, and have a `rollout-type` label (`Step` or `Background`).

Revisions can be longer than a label value allows, so the `rollout-plugin-revision` label holds a hash of the revision,
and the `rolloutplugin.argoproj.io/revision` annotation holds the full revision. To list the AnalysisRuns of a
revision, take the label value from one of them:

```bash
kubectl get analysisruns -l rollout-plugin-revision=<hash>
```
