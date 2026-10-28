# StatefulSet Plugin

!!! warning "Alpha Feature"

    The `RolloutPlugin` API and the StatefulSet plugin are alpha and may change in future releases.

The built-in StatefulSet plugin runs canary rollouts of `apps/v1` StatefulSets by driving their
`spec.updateStrategy.rollingUpdate.partition`. It runs inside the controller (no separate binary).

## Enabling the plugin

Enable the [RolloutPlugin controller](index.md#enabling-the-rolloutplugin-controller), and add the plugin to the
`argo-rollouts-config` ConfigMap:

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
      args:
        - "--loglevel"
        - "debug"
```

| Flag          | Description                                                                                       |
| ------------- | ------------------------------------------------------------------------------------------------- |
| `--loglevel`  | Log level for this plugin only: `debug`, `info`, `warn` or `error`. Defaults to the controller's. |
| `--logformat` | Log format for this plugin only: `text` or `json`. Defaults to the controller's.                  |

Unknown flags or invalid values make the controller fail at startup. The plugin has no `spec.plugin.config` fields:
a RolloutPlugin with a non-empty `config` is invalid.

## Prerequisites

The StatefulSet must use the `RollingUpdate` update strategy (the default).

```yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: my-app
spec:
  replicas: 5
  updateStrategy:
    type: RollingUpdate
  ...
---
apiVersion: argoproj.io/v1alpha1
kind: RolloutPlugin
metadata:
  name: my-app-rollout
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
```

## How weights map to pods

The StatefulSet controller only updates the pods whose index (counted from `spec.ordinals.start`, if set) is at or
above the partition, highest index first. For a weight, the plugin rounds the number of pods to update up, so any
weight above 0 updates at least one pod, and sets the partition to the number of pods that stay on the stable
revision:

| Replicas | `setWeight` | Updated pods | Partition    | Pods on the new revision |
| -------- | ----------- | ------------ | ------------ | ------------------------ |
| 5        | 20          | 1            | 4            | `my-app-4`               |
| 5        | 50          | 3            | 2            | `my-app-2` to `my-app-4` |
| 5        | 100         | 5            | 0            | all                      |
| 4        | 10          | 1            | 3            | `my-app-3`               |
| any      | 0           | 0            | `2147483647` | none                     |

A weight of 0 sets the partition to `2147483647` (the maximum), which is also how the plugin _blocks_ the StatefulSet
between rollouts: any pod the StatefulSet controller creates, for example on a scale-up, then runs the stable
revision.

Revisions are the StatefulSet's controller revisions: `status.currentRevision` is the stable revision and
`status.updateRevision` is the one being rolled out. Each pod carries its revision in the `controller-revision-hash`
label, so an analysis can select the canary pods with the `podTemplateHashValue: Latest` argument.

## How each action is implemented

The plugin only writes the partition, using Server-Side Apply with the field manager
`argo-rollouts-statefulset-plugin`, and deletes pods when they need to move back to an older revision. It changes one
thing at a time, the partition or a single pod, and waits for each change to take effect before the next.

| Action                       | What the plugin does                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| ---------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Validation                   | Requires `workloadRef` to be an `apps/v1` `StatefulSet`, and `spec.plugin.config` to be empty.                                                                                                                                                                                                                                                                                                                                                        |
| Start a rollout              | A change to the pod template creates a new StatefulSet revision, which starts a rollout. Other changes, such as `replicas`, don't.                                                                                                                                                                                                                                                                                                                    |
| `setWeight` step             | Sets the partition for the weight, then [replaces pods](#replacing-pods) as needed. The step completes once the StatefulSet controller has applied the partition, every pod below it runs the stable revision, and enough pods at or above it run the new revision and are available (ready for `minReadySeconds`).                                                                                                                                   |
| `pause` and `analysis` steps | Keeps the partition and the pods at the weight of the last `setWeight` step.                                                                                                                                                                                                                                                                                                                                                                          |
| Completion, promote full     | Waits for the StatefulSet controller to catch up with the latest spec, then sets the partition to 0 and [replaces pods](#replacing-pods) as needed. Once every pod runs the new revision and is ready, blocks the partition again. If the pod template changed again in the meantime, it blocks the partition instead, so the newer revision waits for its own rollout.                                                                               |
| Abort                        | Blocks the partition, then rolls the pods back to the stable revision one at a time, highest index first: deletes a pod on another revision, and waits for it to be recreated on the stable revision and become ready before the next. Progress is read from the pods, so an abort resumes after a controller restart. If every pod already runs the new revision, for example after any weight with a single replica, there is nothing to roll back. |
| Restart                      | Blocks the partition, so the restarted rollout starts with no updated pods.                                                                                                                                                                                                                                                                                                                                                                           |
| Roll back                    | Same as completion, on the stable revision.                                                                                                                                                                                                                                                                                                                                                                                                           |
| Scale                        | Recomputes the partition for the current weight, and rolls back new-revision pods that end up below it. See [Scaling](#scaling).                                                                                                                                                                                                                                                                                                                      |
| Delete                       | See [Deleting a RolloutPlugin](#deleting-a-rolloutplugin).                                                                                                                                                                                                                                                                                                                                                                                            |

Promote, pause, resume, editing steps and a timeout need nothing else from the plugin: they only change which of the
actions above runs next. A timeout with `timeoutAbort: true` runs an abort.

### Replacing pods

The StatefulSet controller updates pods at or above the partition, but never rolls pods below it back. When a lower
weight or a scale-up leaves new-revision pods below the partition, the plugin deletes them one at a time, highest index
first, and the StatefulSet controller recreates them on the stable revision. It waits while any pod is missing,
terminating or not ready, so at most one pod is down at a time.

With the default `OrderedReady` pod management policy, the StatefulSet controller doesn't replace a pod that isn't
ready, so a broken canary would block every later revision. The plugin deletes such a pod when it runs an outdated
revision at or above the partition, so the StatefulSet controller recreates it on the new revision. This is the manual
step Kubernetes documents for a
[forced rollback](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/#forced-rollback).

### Ready

The plugin reports the StatefulSet as ready when:

* the StatefulSet controller has caught up with the latest spec, including the plugin's own last change,
* every replica is ready, and
* during a rollout, every pod runs either the stable or the new revision.

A rollout completes once the StatefulSet is ready and every pod runs the new revision, and an abort completes once it
is ready and no pod runs the new revision.

### Scaling

Scaling the StatefulSet during a rollout recomputes the partition for the current weight. For example, scaling from 4
to 8 replicas at `setWeight: 25` moves the partition from 3 to 6: `my-app-6` and `my-app-7` run the new revision,
and `my-app-3`, now below the partition, is rolled back. Between rollouts the partition stays blocked, so new pods run
the stable revision.

## Argo CD

The plugin only writes the partition. Leave `partition` out of the StatefulSet in git, as you would leave out
`replicas` for a workload scaled by an HPA, and Argo CD neither tracks nor resets it.

If git does set `partition` (for example, a Helm chart renders it), ignore the fields the plugin owns on ArgoCD, so that Argo CD
neither reports the partition as drift nor resets it on sync:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
spec:
  ignoreDifferences:
  - group: apps
    kind: StatefulSet
    managedFieldsManagers:
    - argo-rollouts-statefulset-plugin
  syncPolicy:
    syncOptions:
    - RespectIgnoreDifferences=true
```

See [Argo CD integration](index.md#argo-cd) for the RolloutPlugin health check and actions.

## Deleting a RolloutPlugin

When a RolloutPlugin is deleted, the plugin gives up its ownership of the partition field, and leaves the partition
value as it is. If git sets the partition, the StatefulSet then shows as OutOfSync with the Argo CD settings above,
and a sync restores it. Otherwise, set the partition yourself (for example to 0) if you stop using a RolloutPlugin for
the StatefulSet.

## RBAC

The plugin uses the controller's service account, which needs `get`, `list`, `watch`, `update` and `patch` on
`statefulsets`, and `get`, `list`, `watch` and `delete` on `pods`. The install manifests include them. With
`--namespaced`, the plugin only watches the controller's namespace.

## Limitations

* Only `apps/v1` StatefulSets with the `RollingUpdate` update strategy. With `OnDelete`, the RolloutPlugin reports an
  error.
* The weight is a percentage of pods, rounded up; there is no traffic routing. The canary pods are always the
  highest indexes.
* Pods are rolled back one at a time, and each must become ready before the next.
* Rolling back a pod doesn't touch its PersistentVolumeClaim: a new revision must leave its data usable by the stable
  revision.
* The plugin has no configuration (`spec.plugin.config`) as of now.
