# Scaledown New Replicaset on Aborted Rollout

Upon an aborted update, we may scale down the new replicaset for all strategies. Users can then choose to leave the new replicaset scaled up indefinitely by setting abortScaleDownDelaySeconds to 0, or adjust the value to something larger (or smaller).

The following table summarizes the behavior under combinations of rollout strategy and `abortScaleDownDelaySeconds`. Note that `abortScaleDownDelaySeconds` is not applicable to argo-rollouts v1.0.
`abortScaleDownDelaySeconds = nil` is the default, which means in v1.1 across all rollout strategies, the new replicaset
is scaled down in 30 seconds on abort by default.

|                                    strategy |         v1.0 behavior         | abortScaleDownDelaySeconds |         v1.1 behavior         |
|--------------------------------------------:|:-----------------------------:|:--------------------------:|:-----------------------------:|
|                                  blue-green | does not scale down           | nil                        | scales down after 30 seconds  |
|                                  blue-green | does not scale down           | 0                          | does not scale down           |
|                                  blue-green | does not scale down           | N                          | scales down after N seconds   |
|                                basic canary | rolling update back to stable | N/A                        | rolling update back to stable |
|                   canary w/ traffic routing | scales down immediately       | nil                        | scales down after 30 seconds  |
|                   canary w/ traffic routing | scales down immediately       | 0                          | does not scale down           |
|                   canary w/ traffic routing | scales down immediately       | N                          | scales down after N seconds   |
| canary w/ traffic routing  + setCanaryScale | does not scale down (bug)     | *                          | should behave like  canary w/ traffic routing     |
