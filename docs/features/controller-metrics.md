# Controller Metrics

The Rollouts controller publishes the following prometheus metrics.

| Name                      | Description |
| ------------------------- | ----------- |
| `rollout_created_time`    | Creation time in unix timestamp for an rollout. |
| `rollout_info`            | Information about rollout. |
| `rollout_phase`           | Information on the state of the rollout. |
| `rollout_reconcile`       | Rollout reconciliation performance. |
| `rollout_reconcile_error` | Error occurring during the rollout. |