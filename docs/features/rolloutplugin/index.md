# RolloutPlugin

!!! warning "Alpha Feature (Since 1.11.0)"

    This is an experimental, [alpha-quality](https://github.com/argoproj/argoproj/blob/main/community/feature-status.md#alpha)
    feature. The API (`RolloutPlugin` CRD and the resource plugin interface) may change in future releases.

A `RolloutPlugin` brings Argo Rollouts' canary strategy (weighted steps, pauses, analysis, abort, promote) to workloads
other than Deployments, such as StatefulSets. Unlike a [Rollout](../specification.md), it does not replace the workload:
the workload stays as it is, and the `RolloutPlugin` references it through `spec.workloadRef`. A **resource plugin**
then controls how the workload's pods move to a new revision.

Argo Rollouts ships a built-in [StatefulSet plugin](statefulset.md). Other workload kinds can be supported by
[writing a resource plugin](plugins.md).

!!! info "Built-in plugins are only for native Kubernetes workloads"

    Only plugins for native Kubernetes workload kinds, such as StatefulSets or DaemonSets, are considered for being
    built into Argo Rollouts. Any other workload, such as a custom resource, must be supported with an
    [external resource plugin](plugins.md).

## How it works

The work is split between the RolloutPlugin controller and the resource plugin.

The RolloutPlugin controller works the same way for every kind of workload. It:

* starts a rollout when the workload has a new revision,
* runs the canary steps, pauses, and step and background analysis,
* handles promote, promote full, pause, resume, abort and restart,
* enforces `timeoutSeconds` and `timeoutAbort`,
* maintains the status, events, notifications and metrics.

The resource plugin knows how to update one kind of workload. It:

* reports the workload's stable and new revisions, replica counts and readiness,
* moves a percentage of the pods, or every pod, to the new revision, and reports when that's done,
* rolls every pod back to the stable revision when the rollout is aborted,
* releases the workload when the RolloutPlugin is deleted.

A rollout starts whenever the workload has a new revision, for example after the StatefulSet's pod template is
changed. The controller then executes the canary steps in order and completes the rollout once every pod runs the new
revision and is ready. See [Actions and Lifecycle](actions.md) for everything that can happen during a rollout.

### Differences from a Rollout

* Only the canary strategy with `setWeight`, `pause` and `analysis` steps is supported. Blue-green, traffic routing,
  experiments, `setCanaryScale`, step plugins and anti-affinity are not yet supported.
* The workload is referenced, not embedded. The `RolloutPlugin` never creates or deletes the workload.
* The `kubectl argo rollouts` plugin and the dashboard do not support RolloutPlugins yet. Use `kubectl patch` or the
  [Argo CD actions](#argo-cd) to control a rollout.

## Enabling the RolloutPlugin controller

The RolloutPlugin controller is disabled by default. Enabling it takes three steps.

1. Enable the controller with the `--controllers` flag of the `argo-rollouts` controller. `--controllers=rolloutplugin`
   runs the default controllers plus the RolloutPlugin controller (`--controllers=analysis,rolloutplugin` runs only the
   analysis and RolloutPlugin controllers):

    ```yaml
    apiVersion: apps/v1
    kind: Deployment
    metadata:
      name: argo-rollouts
      namespace: argo-rollouts
    spec:
      template:
        spec:
          containers:
          - name: argo-rollouts
            args:
            - --controllers=rolloutplugin
    ```

2. Enable the resource plugins you use in the `argo-rollouts-config` ConfigMap under the `resourcePlugins` key. The
   built-in StatefulSet plugin uses the `builtin://` location; external plugins use `file://` or `http(s)://` like
   [other plugins](../../plugins.md):

    ```yaml
    apiVersion: v1
    kind: ConfigMap
    metadata:
      name: argo-rollouts-config
      namespace: argo-rollouts
    data:
      resourcePlugins: |-
        - name: "argoproj/statefulset"
          location: "builtin://statefulset"
    ```

3. Restart the controller. The ConfigMap is only read at startup.

The RolloutPlugin controller runs in the same process as the other controllers, and only on the leader replica. If a
plugin fails to start, the controller exits. `--namespaced` and `--instance-id` apply to RolloutPlugins as they do to
Rollouts.

A `RolloutPlugin` referencing a plugin that is not enabled (missing, or listed with `disabled: true`) is invalid, and
its workload isn't touched until the plugin is enabled.

### RBAC

The install manifests grant the controller access to `rolloutplugins` and to what the built-in StatefulSet plugin
needs (`statefulsets`: get, list, watch, update, patch; `pods`: get, list, watch, delete). External plugins run with
the controller's service account, so grant it whatever access the plugin's workload kind requires, including
`list` and `watch`, which the controller needs to watch that kind.

## Example

A 5-replica StatefulSet with a canary rollout: update 20% of the pods, wait for approval, update 60%, soak for
5 minutes, then update all pods.

```yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: my-app
spec:
  serviceName: my-app
  replicas: 5
  selector:
    matchLabels:
      app: my-app
  template:
    metadata:
      labels:
        app: my-app
    spec:
      containers:
      - name: my-app
        image: my-registry/my-app:v1.0.0
  updateStrategy:
    type: RollingUpdate
---
apiVersion: argoproj.io/v1alpha1
kind: RolloutPlugin
metadata:
  name: my-app
spec:
  workloadRef:
    apiVersion: apps/v1
    kind: StatefulSet
    name: my-app
  plugin:
    name: argoproj/statefulset
  strategy:
    canary:
      steps:
      - setWeight: 20
      - pause: {}
      - setWeight: 60
      - pause: {duration: 5m}
      - setWeight: 100
```

Change the StatefulSet's pod template (for example its image) to start a rollout. See the
[RolloutPlugin specification](specification.md) for every field.

## Checking the status

```bash
kubectl get rolloutplugin my-app      # or: kubectl get rp my-app
```

```text
NAME     STATUS        PLUGIN                 AGE
my-app   Progressing   argoproj/statefulset   3m
```

`kubectl describe rolloutplugin my-app` shows the current step, the status message and the events.

| Phase         | Meaning                                                                                                                                         |
| ------------- | ----------------------------------------------------------------------------------------------------------------------------------------------- |
| `Healthy`     | No rollout in progress; every pod runs the stable revision.                                                                                     |
| `Progressing` | A rollout is executing its steps, or pods are being updated to a revision.                                                                      |
| `Paused`      | Paused at a pause step, manually paused (`spec.paused`), or paused by an inconclusive analysis.                                                 |
| `Degraded`    | The spec is invalid, the rollout was aborted (manually, by a failed analysis or by `timeoutAbort`), timed out, or hit an error that is retried. |

## Integrations

### Argo CD

Add a health check and resource actions for `RolloutPlugin` to the `argocd-cm` ConfigMap. The actions patch the same
fields as the `kubectl` commands in [Actions and Lifecycle](actions.md):

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: argocd-cm
  namespace: argocd
data:
  resource.customizations.health.argoproj.io_RolloutPlugin: |
    hs = {}
    local healthByPhase = {Healthy = "Healthy", Progressing = "Progressing", Paused = "Suspended", Degraded = "Degraded"}
    if obj.status ~= nil and healthByPhase[obj.status.phase] ~= nil then
      hs.status = healthByPhase[obj.status.phase]
      hs.message = obj.status.message or ("RolloutPlugin is " .. obj.status.phase)
    else
      hs.status = "Unknown"
      hs.message = "No status available"
    end
    return hs

  resource.customizations.actions.argoproj.io_RolloutPlugin: |
    discovery.lua: |
      local actions = {}
      local phase, stepPaused, aborted = "", false, false
      if obj.status ~= nil then
        phase = obj.status.phase or ""
        stepPaused = obj.status.pauseConditions ~= nil and #obj.status.pauseConditions > 0
        aborted = obj.status.aborted == true
      end
      local specPaused = obj.spec ~= nil and obj.spec.paused == true
      local active = phase == "Progressing" or phase == "Paused" or stepPaused or specPaused
      actions["pause"] = {["disabled"] = phase ~= "Progressing" or stepPaused or specPaused}
      actions["resume"] = {["disabled"] = not (stepPaused or specPaused) or aborted}
      actions["promoteFull"] = {["disabled"] = not active or aborted}
      actions["abort"] = {["disabled"] = not active or aborted}
      actions["restart"] = {["disabled"] = not aborted}
      return actions
    definitions:
    - name: pause
      action.lua: |
        obj.spec.paused = true
        return obj
    - name: resume
      action.lua: |
        if obj.status ~= nil then
          obj.status.pauseConditions = nil
        end
        if obj.spec.paused == true then
          obj.spec.paused = false
        end
        return obj
    - name: promoteFull
      action.lua: |
        obj.status = obj.status or {}
        obj.status.promoteFull = true
        return obj
    - name: abort
      action.lua: |
        obj.status = obj.status or {}
        obj.status.abort = true
        return obj
    - name: restart
      action.lua: |
        obj.status = obj.status or {}
        obj.status.restart = true
        return obj
```

This example disables `promoteFull` for an aborted rollout; remove `or aborted` from its rule to allow promoting an
aborted rollout, which the controller supports. If your Argo CD installation restricts RBAC, grant the
`argocd-server` and `argocd-application-controller` service accounts `get`, `list`, `watch`, `update` and `patch` on
`rolloutplugins` and `rolloutplugins/status`.

Resource plugins write a field of the workload (the built-in StatefulSet plugin writes the partition). Leave that field
out of git, or have Argo CD ignore it; see [the StatefulSet plugin](statefulset.md#argo-cd).

### Notifications

RolloutPlugins send [notifications](../notifications.md#rolloutplugin) when a rollout starts, pauses, completes, is
aborted and more, for example with the `on-rollout-plugin-completed` trigger.

### Metrics

The controller publishes [RolloutPlugin metrics](../controller-metrics.md#available-metrics-for-rolloutplugin-objects)
such as `rolloutplugin_info` and `rolloutplugin_reconcile`.

## More examples

Manual gates between weights:

```yaml
strategy:
  canary:
    steps:
    - setWeight: 10
    - pause: {}
    - setWeight: 30
    - pause: {}
    - setWeight: 100
```

Analysis between weights; the rollout only advances when the analysis succeeds, and aborts when it fails:

```yaml
strategy:
  canary:
    steps:
    - setWeight: 25
    - analysis:
        templates:
        - templateName: http-benchmark
    - setWeight: 75
    - analysis:
        templates:
        - templateName: http-benchmark
    - setWeight: 100
```

Background analysis for the whole rollout, and an automatic abort when the rollout stops making progress:

```yaml
spec:
  timeoutSeconds: 600
  timeoutAbort: true
  strategy:
    canary:
      analysis:
        templates:
        - templateName: error-rate
      steps:
      - setWeight: 20
      - pause: {duration: 10m}
      - setWeight: 60
      - pause: {duration: 10m}
      - setWeight: 100
```

## Limitations

* Canary strategy only (see [Differences from a Rollout](#differences-from-a-rollout)).
* No `kubectl argo rollouts` or dashboard support.
* `resourcePlugins` entries are only read at controller startup.
* One RolloutPlugin per workload. The controller doesn't detect two RolloutPlugins referencing the same workload.
* The built-in StatefulSet plugin requires the `RollingUpdate` update strategy (see [its limitations](statefulset.md#limitations)).
