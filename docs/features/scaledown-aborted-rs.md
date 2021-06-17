# Scaledown New Replicaset on Aborted Rollout

Upon an abort, we should scale down the new replicaset for all strategies. Users can then choose to leave the new replicaset scaled up indefinitely by setting abortScaleDownDelaySeconds to 0, or adjust the value to something larger (or smaller).

`abortScaleDownDelaySeconds = nil` is the default.

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
