# Controller Metrics

The Argo Rollouts controller publishes the following prometheus metrics about Argo Rollout objects.

| Name                                | Description |
| ----------------------------------- | ----------- |
| `rollout_created_time`              | Creation time in unix timestamp for an rollout. |
| `rollout_info`                      | Information about rollout. |
| `rollout_info_replicas_available`   | The number of available replicas per rollout. |
| `rollout_info_replicas_unavailable` | The number of unavailable replicas per rollout. |
| `rollout_phase`                     | Information on the state of the rollout. |
| `rollout_reconcile`                 | Rollout reconciliation performance. |
| `rollout_reconcile_error`           | Error occurring during the rollout. |
| `experiment_created_time`           | Creation time in unix timestamp for an experiment. |
| `experiment_info`                   | Information about Experiment. |
| `experiment_phase`                  | Information on the state of the experiment. |
| `experiment_reconcile`              | Experiments reconciliation performance. |
| `experiment_reconcile_error`        | Error occurring during the experiment. |
| `analysis_run_created_time`         | Creation time in unix timestamp for an Analysis Run. |
| `analysis_run_info`                 | Information about analysis run. |
| `analysis_run_metric_phase`         | Information on the duration of a specific metric in the Analysis Run. |
| `analysis_run_metric_type`          | Information on the type of a specific metric in the Analysis Runs. |
| `analysis_run_phase`                | Information on the state of the Analysis Run. |
| `analysis_run_reconcile`            | Analysis Run reconciliation performance. |
| `analysis_run_reconcile_error`      | Error occurring during the analysis run. |

The controller also publishes the following Prometheus metrics to describe the controller health.

| Name                                          | Description |
| --------------------------------------------- | ----------- |
| `controller_clientset_k8s_request_total`      | Number of kubernetes requests executed during application reconciliation. |
| `workqueue_adds_total`                        | Total number of adds handled by workqueue |
| `workqueue_depth`                             | Current depth of workqueue |
| `workqueue_queue_duration_seconds`            | How long in seconds an item stays in workqueue before being requested. |
| `workqueue_work_duration_seconds`             | How long in seconds processing an item from workqueue takes. |
| `workqueue_unfinished_work_seconds`           | How many seconds of work has done that is in progress and hasn't been observed by work_duration. Large values indicate stuck threads. One can deduce the number of stuck threads by observing the rate at which this increases. |
| `workqueue_longest_running_processor_seconds` | How many seconds has the longest running processor for workqueue been running |
| `workqueue_retries_total`                     | Total number of retries handled by workqueue |

In additional, the Argo Rollouts controllers offers metrics on CPU, memory and file descriptor usage as well as the process start time and current Go processes including memory stats.