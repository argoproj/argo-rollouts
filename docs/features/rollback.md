# Rollback Windows

!!! important

    Available for blue-green and canary rollouts since v1.4

By default, when an older Rollout manifest is re-applied, the controller treats it the same as a spec change, and will execute the full list of steps, and perform analysis too. There are two exceptions to this rule: 
1. the controller detects if it is moving back to a blue-green ReplicaSet which exists and is still scaled up (within its `scaleDownDelay`) 
2. the controller detects it is moving back to the canary's "stable" ReplicaSet, and the upgrade had not yet completed.

It is often undesirable to re-run analysis and steps for a rollout, when the desired behavior is to rollback as soon as possible. To help with this, a rollback window feature allows users to indicate that the promotion to the ReplicaSet within the window will skip all steps.

Example:

```yaml
spec:
  rollbackWindow:
    revisions: 3

  revisionHistoryLimit: 5
```

Assume a linear revision history: `1`, `2`, `3`, `4`, `5 (current)`. A rollback from revision 5 back to 4 or 3 will fall within the window, so it will be fast tracked.
