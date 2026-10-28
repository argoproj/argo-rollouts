# RolloutPlugin Actions and Lifecycle

This page describes everything that can be done to a `RolloutPlugin` and what happens in response. The actions work
the same way for every plugin; only what happens to the workload depends on the plugin. For the built-in StatefulSet
plugin, see [How each action is implemented](statefulset.md#how-each-action-is-implemented).

## Summary

| Action                                        | How                                                      |
| --------------------------------------------- | -------------------------------------------------------- |
| [Start a rollout](#start-a-rollout)           | Update the workload to a new revision                    |
| [Set weight](#steps)                          | `setWeight` step                                         |
| [Pause step](#steps), [analysis step](#steps) | `pause` or `analysis` step                               |
| [Complete](#completion)                       | Automatic after the last step                            |
| [Promote](#promote)                           | Clear `status.pauseConditions`                           |
| [Promote full](#promote-full)                 | `status.promoteFull: true`                               |
| [Pause](#pause)                               | `spec.paused: true`                                      |
| [Resume](#resume)                             | `spec.paused: false`                                     |
| [Abort](#abort)                               | `status.abort: true`, failed analysis, or `timeoutAbort` |
| [Restart](#restart)                           | `status.restart: true` after an abort                    |
| [Roll back](#roll-back)                       | Revert the workload to the stable revision               |
| [Edit steps](#edit-steps)                     | Change `spec.strategy.canary.steps`                      |
| [Scale](#scale)                               | Change the workload's replicas                           |
| [Time out](#timeout)                          | No progress for `spec.timeoutSeconds`                    |
| [Delete](#delete)                             | Delete the `RolloutPlugin`                               |

Promote, promote full, abort and restart are requested on the `status` subresource, which needs
`--subresource=status` (kubectl 1.24 or later). With Argo CD, use the [resource actions](index.md#argo-cd) instead.

Users need `patch` on `rolloutplugins/status` for these actions, and on `rolloutplugins` to set `spec.paused`. The
install manifests aggregate both into the default `edit` and `admin` cluster roles; `view` gets read-only access.

## Start a rollout

Update the workload so that it has a new revision, for example by changing a StatefulSet's image. Which changes
create a new revision depends on the plugin. Once the plugin reports the new revision, the phase becomes `Progressing`
and the steps start from the first step.

A new revision deployed while a rollout is in progress replaces it: pauses, a pending promote full and the restart
count are cleared, the running analysis is replaced, and the steps start again from the first step. An aborted rollout
stays aborted for its own revision, but a new revision starts normally.

## Steps

Steps run in order, and `status.currentStepIndex` shows the current step.

* **`setWeight`** asks the plugin to move the given percentage of the workload to the new revision, and waits until
  the plugin confirms the weight is reached. How a weight is applied depends on the plugin; the StatefulSet plugin,
  for example, [updates that percentage of pods](statefulset.md#how-weights-map-to-pods). A weight that can't be
  reached, for example because the new revision fails to start, stalls the step until `timeoutSeconds`.
* **`pause`** pauses the rollout (phase `Paused`). With a `duration`, the rollout continues once the duration has
  elapsed. Without one, it waits for a [promote](#promote).
* **`analysis`** creates an AnalysisRun and waits for it: `Successful` advances, `Failed` or `Error`
  [aborts](#abort), and `Inconclusive` pauses the rollout. Promoting then runs the analysis again.

During `pause` and `analysis` steps, the workload stays at the weight of the last `setWeight` step (0 if none), also
if it is [scaled](#scale).

A [background analysis](specification.md#strategycanaryanalysis) runs alongside the steps from `startingStep`. It
aborts or pauses the rollout in the same way as a step analysis.

## Completion

After the last step (or a [promote full](#promote-full)), the plugin moves the whole workload to the new revision. Once
the plugin reports that every replica runs it and the workload is ready, the rollout completes: the phase becomes
`Healthy` and the new revision becomes the stable revision.

If `spec.paused` is set, completion waits until the rollout is resumed.

## Promote

Approves the current pause: an indefinite or timed `pause` step, or a pause caused by an inconclusive analysis.

```bash
kubectl patch rolloutplugin my-app-rollout \
  --type=merge --subresource=status \
  -p '{"status":{"pauseConditions":null}}'
```

After a `pause` step, the rollout moves to the next step. After an inconclusive analysis, the analysis runs again.

## Promote full

Skips all remaining steps and analysis and moves the whole workload to the new revision.

```bash
kubectl patch rolloutplugin my-app-rollout \
  --type=merge --subresource=status \
  -p '{"status":{"promoteFull":true}}'
```

Promote full clears any pause (including `spec.paused`), stops the running analysis, and overrides an abort or a
timeout, so it also promotes an aborted or timed-out rollout. The rollout then [completes](#completion).

As in Rollouts, `status.promoteFull` stays `true` until every replica runs the new revision and the workload is ready;
until then, a
pause or an abort requested in the meantime is overridden. It only applies to the revision being rolled out: if a
newer revision is deployed first, the request is cleared and the newer revision starts from the first step. With no
rollout in progress, the request is ignored and cleared.

## Pause

Freezes the rollout at its current position:

```bash
kubectl patch rolloutplugin my-app-rollout \
  --type=merge \
  -p '{"spec":{"paused":true}}'
```

While paused (phase `Paused`):

* No weight is applied and no step completes. A pause step's duration still counts from when the step started, so a
  duration that elapsed while paused completes the step on resume.
* The paused time doesn't count toward `timeoutSeconds`.
* Analysis keeps running, and a failed analysis still aborts the rollout.
* Abort and promote full still work; promote full also resumes the rollout.

## Resume

Resumes a manually paused rollout:

```bash
kubectl patch rolloutplugin my-app-rollout \
  --type=merge \
  -p '{"spec":{"paused":false}}'
```

Resuming restarts the `timeoutSeconds` countdown. The Argo CD `resume` action also clears `status.pauseConditions`, so
it doubles as [promote](#promote).

## Abort

Stops the rollout and rolls the workload back to the stable revision:

```bash
kubectl patch rolloutplugin my-app-rollout \
  --type=merge --subresource=status \
  -p '{"status":{"abort":true}}'
```

The running analysis is stopped, and the plugin rolls the workload back. Once the plugin reports that no replica runs
the new revision and the workload is ready, the rollout is marked aborted (`status.aborted`) and the phase becomes
`Degraded`. If the rollback fails, it is retried.

A rollout is also aborted automatically when an analysis fails, or when it [times out](#timeout) with
`timeoutAbort: true`.

If the plugin already reports the new revision as the stable revision (for example after `setWeight: 100`), there is
nothing to roll back to. A manual abort is then ignored. An automatic abort is still recorded: the rollout ends
`Degraded` and the workload stays on the new revision. To go back, [roll back](#roll-back) the workload instead.

An aborted rollout stays aborted for its revision. Deploy a new revision, [restart](#restart) it, or
[promote it fully](#promote-full).

## Restart

Restarts an aborted rollout from the first step with the same revision:

```bash
kubectl patch rolloutplugin my-app-rollout \
  --type=merge --subresource=status \
  -p '{"status":{"restart":true}}'
```

The plugin moves the workload back to 0% of the new revision, the abort is cleared, and the steps run again from the
first step. `status.restartCount` counts the restarts of the current revision.

Restart only applies to an aborted rollout; otherwise the request is ignored and cleared. If an abort is still rolling
the workload back, the restart waits for it to finish. If the plugin fails to restart, the error is shown in the status message
and the request is cleared.

## Roll back

Revert the workload to the stable revision, for example by reverting a StatefulSet's image with `git revert` and an
Argo CD sync. The remaining steps are skipped, any abort is cleared, and the rollout [completes](#completion) on the
stable revision, so the canary is moved back straight away.

## Edit steps

Changing `spec.strategy.canary.steps` during a rollout, including after the last step while the workload is still
updating, restarts the rollout from the first of the new steps. The rollout's revision and abort state are unchanged.

## Scale

Scaling the workload during a rollout doesn't restart it. The current weight is applied to the new number of
replicas (see [Scaling](statefulset.md#scaling) for the StatefulSet plugin).

## Timeout

If a rollout makes no progress for `spec.timeoutSeconds`, it is marked as timed out, the phase becomes
`Degraded`, and the running analysis is stopped. What counts as progress is listed in the
[specification](specification.md#timeoutseconds-and-timeoutabort).

A timed-out rollout holds where it is. It continues after a new revision, promote full, pause and resume, or abort and
restart. With `timeoutAbort: true` the rollout is [aborted](#abort) instead.

## Delete

The controller adds a finalizer to every RolloutPlugin, so that on deletion the plugin can release the workload first
(see [Deleting a RolloutPlugin](statefulset.md#deleting-a-rolloutplugin) for the StatefulSet plugin). The workload
itself is not deleted; the RolloutPlugin's AnalysisRuns are.

If the cleanup fails, a warning event is recorded and the cleanup is retried. If the plugin is no longer enabled, the
cleanup is skipped. If the RolloutPlugin controller isn't running, the deletion waits; remove the finalizer to let it
go:

```bash
kubectl patch rolloutplugin my-app-rollout --type json -p '[{"op":"remove","path":"/metadata/finalizers"}]'
```

## When requests conflict

* An invalid spec blocks everything else until it is fixed.
* Promote full overrides a pause, an abort and a timeout.
* A new revision replaces the current rollout, including an aborted or timed-out one.
* Abort and promote full still work while the rollout is paused or timed out.
