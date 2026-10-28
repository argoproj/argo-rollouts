# Writing a Resource Plugin

!!! warning "Alpha Feature"

    The resource plugin interface is alpha and may change in future releases.

A resource plugin teaches the [RolloutPlugin](index.md) controller how to run a canary on one workload kind. The
controller runs the steps, analysis, pauses, user actions, timeouts and status; the plugin translates them into changes
to the workload: move a percentage of it to the new revision, promote it fully, roll it back, and report its state.

Resource plugins are [hashicorp go-plugin](https://github.com/hashicorp/go-plugin) executables, like the other
[Argo Rollouts plugins](../../plugins.md), which describes the plugin naming, repository and loading conventions. A
plugin for a native Kubernetes workload kind can also be built into the controller, like the
[StatefulSet plugin](statefulset.md); see [Built-in plugins](#built-in-plugins).

## The interface

External plugins implement `RpcResourcePlugin` from
[`utils/plugin/types`](https://github.com/argoproj/argo-rollouts/blob/master/utils/plugin/types/types.go):

```go
type RpcResourcePlugin interface {
	// InitPlugin is called once the controller replica becomes the leader, with its watch namespace
	// (empty for cluster-wide). Create clients and start watches here.
	InitPlugin(namespace string) RpcError
	// WatchedGVK returns the workload kind the plugin manages. Called at startup, before InitPlugin.
	WatchedGVK() (WatchedGVK, RpcError)
	// Validate checks the plugin-specific parts of the spec (workloadRef, plugin.config).
	Validate(rolloutPlugin *v1alpha1.RolloutPlugin) RpcError
	// GetResourceStatus reports the state of the workload.
	GetResourceStatus(rolloutPlugin *v1alpha1.RolloutPlugin) (*ResourceStatus, RpcError)
	// SetWeight moves weight% of the workload to the updated revision.
	SetWeight(rolloutPlugin *v1alpha1.RolloutPlugin, weight int32) RpcError
	// VerifyWeight reports whether weight% has been reached.
	VerifyWeight(rolloutPlugin *v1alpha1.RolloutPlugin, weight int32) (bool, RpcError)
	// PromoteFull moves the whole workload to the updated revision.
	PromoteFull(rolloutPlugin *v1alpha1.RolloutPlugin) RpcError
	// Abort moves the whole workload back to the current (stable) revision.
	Abort(rolloutPlugin *v1alpha1.RolloutPlugin) RpcError
	// Restart prepares an aborted rollout to start again from step 0.
	Restart(rolloutPlugin *v1alpha1.RolloutPlugin) RpcError
	// DeletionCleanup drops the plugin's claim on the workload when the RolloutPlugin is deleted.
	DeletionCleanup(rolloutPlugin *v1alpha1.RolloutPlugin) RpcError
	// Type returns a name for the plugin.
	Type() string
}

type ResourceStatus struct {
	Replicas          int32
	UpdatedReplicas   int32
	ReadyReplicas     int32
	AvailableReplicas int32
	CurrentRevision   string
	UpdatedRevision   string
	Ready             bool
	ObservedResourceVersion string
}

type WatchedGVK struct {
	Group   string
	Version string
	Kind    string
}
```

Every workload method receives the whole RolloutPlugin, including its namespace, `spec.workloadRef` and
`spec.plugin.config`, and its status. Treat it as read-only: the controller owns it.

## Reporting status

`GetResourceStatus` drives the controller, which calls it on every reconcile:

| Field                                                              | Meaning                                                                                                                                                                                                          |
| ------------------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `CurrentRevision`                                                  | The stable revision. It must only change once a rollout completes.                                                                                                                                               |
| `UpdatedRevision`                                                  | The revision of the workload's latest spec. A rollout is in progress while it differs from `CurrentRevision`. A new value starts a new rollout from step 0; a change back to `CurrentRevision` [rolls back](actions.md#roll-back). |
| `Replicas`, `UpdatedReplicas`, `ReadyReplicas`, `AvailableReplicas` | Desired replicas, replicas on `UpdatedRevision`, and ready and available replicas.                                                                                                                              |
| `Ready`                                                            | Every replica is ready, no replica runs a revision other than `CurrentRevision` or `UpdatedRevision` while they differ, and the status reflects the workload's latest spec, including the plugin's own last write. |
| `ObservedResourceVersion`                                          | The workload's `metadata.resourceVersion` the status was computed from. If it is older than the version that triggered the reconcile, the controller retries shortly instead of acting on stale data. Leave it empty to opt out. |

The controller completes a rollout when `CurrentRevision` equals `UpdatedRevision`, `UpdatedReplicas` equals
`Replicas` and `Ready` is true. It completes an abort when `UpdatedReplicas` is 0 and `Ready` is true. If `Ready` is
reported too early, for example from a cache that hasn't seen the plugin's own last write, the controller may complete
before the workload has.

Revision values are free-form strings, but they appear in AnalysisRun names and in the `podTemplateHashValue`
analysis arguments, so keep them short and stable, for example a hash of the workload's template.

## When each method is called

| Method              | Called                                                                                                         | Expected behavior                                                                                                                                                                                                                 |
| ------------------- | -------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `WatchedGVK`        | Once at startup, before `InitPlugin`.                                                                          | Return the kind. The controller watches its metadata and reconciles the RolloutPlugins that reference a changed workload. Plugins managing the same kind share one watch.                                                      |
| `InitPlugin`        | Once the replica becomes the leader, and again after the plugin process restarts.                             | Create clients and caches scoped to the namespace. An error at leader election stops the controller; after a restart, the call is retried.                                                                                    |
| `Validate`          | Every reconcile, before the other workload methods.                                                           | Check `workloadRef` and parse `plugin.config`. An error marks the RolloutPlugin `InvalidSpec` with the message, and nothing else is called until it passes. The controller also checks that `workloadRef` matches `WatchedGVK`. |
| `GetResourceStatus` | Every reconcile, and after the workload-changing methods.                                                      | See [Reporting status](#reporting-status). An error sets `ReconciliationError`; the controller retries.                                                                                                                          |
| `SetWeight`         | Every reconcile of a step: with the step's weight for `setWeight`, and the last `setWeight` (0 if none) for `pause` and `analysis` steps. | Move `weight`% of the workload to `UpdatedRevision`, and the rest to `CurrentRevision`. 0 means none. Keep converging the workload toward the weight on later calls, for example after a scale-up.            |
| `VerifyWeight`      | After `SetWeight` on a `setWeight` step, until it returns `true`.                                              | Return `true` once the weight has been reached and is healthy enough to move on. The step stalls until then.                                                                                                                      |
| `PromoteFull`       | Every reconcile after the last step or a promote full, until the rollout completes.                           | Move the whole workload to `UpdatedRevision`. If the workload's latest revision isn't the RolloutPlugin's `status.updatedRevision`, don't roll it out: a newer revision gets its own rollout.                                  |
| `Abort`             | Every reconcile of an abort, until it completes. Not called when `CurrentRevision` already equals `UpdatedRevision`: there is nothing to roll back to. | Move the whole workload back to `CurrentRevision`. An error sets `ReconciliationError`; the controller retries.                                                                                                                  |
| `Restart`           | Once, when a user restarts an aborted rollout.                                                                 | Undo anything that would skip steps, so the rollout starts again from 0% on the new revision. An error is reported in `status.message` and the restart is cleared.                                                               |
| `DeletionCleanup`   | When the RolloutPlugin is deleted, until it succeeds.                                                          | See [Deleting](#deleting).                                                                                                                                                                                                        |

Other errors make the controller retry the reconcile with backoff.

## Design rules

* **Idempotent, one step at a time.** Methods are called on every reconcile, with the same arguments, until the
  controller sees the result in `GetResourceStatus` or `VerifyWeight`. Do at most one unit of work per call (for
  example one write, or one pod deletion), return, and continue on the next call. Never block waiting for the
  workload.
* **Stateless.** Derive what to do from the workload and the RolloutPlugin on every call. The plugin process can be
  restarted at any time, and the controller can fail over to another replica.
* **Concurrent.** The controller reconciles up to 10 RolloutPlugins in parallel, so methods may run concurrently
  for different RolloutPlugins.
* **Hold new revisions.** Between rollouts, and from step 0 of a rollout, a change to the workload's spec must not
  reach replicas on its own: only `SetWeight` and `PromoteFull` move replicas to a new revision. For example, the
  StatefulSet plugin keeps the partition at its maximum.
* **Own only what you write.** Prefer Server-Side Apply with a dedicated field manager for the fields the plugin
  manages, so tools like Argo CD can ignore them (see [the StatefulSet plugin](statefulset.md#argo-cd)).
* **Reject what you can't handle in `Validate`**, for example an unsupported workload configuration or invalid
  `plugin.config`, so the user sees it as `InvalidSpec` instead of a stuck rollout.

## Deleting

The controller holds a deleted RolloutPlugin with the `rolloutplugin.argoproj.io/deletion-cleanup` finalizer until
`DeletionCleanup` succeeds. It must:

* leave the workload's spec as it is, and only drop the plugin's claim on it, for example its Server-Side Apply field
  ownership, so the workload's owner (such as Argo CD) sees the plugin's values as drift and can restore its own;
* return no error for a missing workload or a `workloadRef` the plugin doesn't manage: it is called even if `Validate`
  never passed.

## Building an external plugin

The [resource plugin sample](https://github.com/argoproj/argo-rollouts/tree/master/test/cmd/resource-plugin-e2e) is a
minimal StatefulSet plugin. Its `main.go` serves the implementation with the resource plugin handshake:

```go
package main

import (
	goPlugin "github.com/hashicorp/go-plugin"

	rolloutsPlugin "github.com/argoproj/argo-rollouts/rolloutplugin/plugin/rpc"
)

var handshakeConfig = goPlugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "ARGO_ROLLOUTS_RPC_PLUGIN",
	MagicCookieValue: "resourceplugin",
}

func main() {
	goPlugin.Serve(&goPlugin.ServeConfig{
		HandshakeConfig: handshakeConfig,
		Plugins: map[string]goPlugin.Plugin{
			"RpcResourcePlugin": &rolloutsPlugin.ResourcePluginImpl{Impl: &MyPlugin{}},
		},
	})
}
```

Register it under `resourcePlugins` in the `argo-rollouts-config` ConfigMap, with the same `location`, `sha256`,
`args` and `disabled` fields as [other plugins](../../plugins.md#plugin-name). The plugin receives `args` as
command-line arguments.

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: argo-rollouts-config
  namespace: argo-rollouts
data:
  resourcePlugins: |-
    - name: "argoproj-labs/my-workload"
      location: "https://github.com/argoproj-labs/rollouts-plugin-resource-my-workload/releases/download/v0.1.0/plugin-linux-amd64"
      sha256: "<sha256 of the binary>"
      args:
        - "--log-level"
        - "debug"
```

A RolloutPlugin then references it by name, and passes any plugin-specific configuration under `config`:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: RolloutPlugin
metadata:
  name: my-workload-rollout
spec:
  workloadRef:
    apiVersion: example.com/v1
    kind: MyWorkload
    name: my-workload
  plugin:
    name: argoproj-labs/my-workload
    config:
      someSetting: value
  strategy:
    canary:
      steps:
      - setWeight: 50
      - pause: {}
```

The controller starts the plugin at startup to call `WatchedGVK`. A plugin that fails to start, or fails to return its
kind, stops the controller from starting. A plugin that crashes later is restarted on its next call.

### RBAC

The plugin runs with the controller's service account. Grant it what the plugin needs on the workload and the
resources it manages, plus `get`, `list` and `watch` on the workload kind for the controller's own watch. For example:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: argo-rollouts-my-workload-plugin
rules:
- apiGroups: ["example.com"]
  resources: ["myworkloads"]
  verbs: ["get", "list", "watch", "patch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: argo-rollouts-my-workload-plugin
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: argo-rollouts-my-workload-plugin
subjects:
- kind: ServiceAccount
  name: argo-rollouts
  namespace: argo-rollouts
```

## Built-in plugins

!!! info "Only native Kubernetes workloads"

    Only plugins for native Kubernetes workload kinds, such as StatefulSets or DaemonSets, are considered for being
    built into Argo Rollouts. Plugins for any other workload, such as a custom resource, must be external plugins.

A plugin contributed to Argo Rollouts can be built into the controller instead of running as a separate process, like
the [StatefulSet plugin](https://github.com/argoproj/argo-rollouts/tree/master/rolloutplugin/plugins/statefulset). It
implements the same methods and is registered in the controller under an ID. Users enable it with
`location: "builtin://<id>"`, and can pass it `args` like an external plugin.

## Testing

* Unit test the plugin against a fake client. The StatefulSet plugin's
  [tests](https://github.com/argoproj/argo-rollouts/tree/master/rolloutplugin/plugins/statefulset) are an example.
* The `TestRolloutPluginRpcSuite` e2e suite runs the sample plugin through the controller: `make setup-e2e` builds it
  and registers it in the ConfigMap, and `make start-e2e` starts the controller with the RolloutPlugin controller
  enabled. See the [contributing guide](../../CONTRIBUTING.md#running-e2e-tests) for the e2e setup.
