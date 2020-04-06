# Rollout Pod Restarts

For various reasons, applications sometimes need a restart. Since the restart is not changing the version, the application should not have to go through the entire BlueGreen or canary deployment process. The Rollout object supports restarting an application by having the controller do a rolling recreate of all the Pods in a Rollout without going through all the regular BlueGreen or Canary deployments. The controller kills one Pod at a time and relies on the ReplicaSet to scale up new Pods until all the Pods are newer than the restarted time.

## How it works

The Rollout object has a field called `.spec.restartAt` that takes in a [RFC 3339 formatted](https://tools.ietf.org/html/rfc3339) string (ie. 2020-03-30T21:19:35Z). If the current time is past the `restartAt` time, the controller knows it needs to restart all the Pods in the Rollout. The controller goes through each ReplicaSet to see if all the Pods have a creation timestamp newer than the `restartAt` time. To prevent too many Pods from restarting at once, the controller limits itself to deleting one Pod at a time and checks that no other Pods in that ReplicaSet have a deletion timestamp and that ReplicaSet is fully available.  The controller checks the ReplicaSets in the following order: 1. stable ReplicaSet, 2. new ReplicaSet, and 3rd. all the other ReplicaSets starting with the oldest. Once the controller has confirmed all the Pods are newer than the `restartAt` time, the controller sets the `status.restartedAt` field to indicate that the Rollout has been successfully restarted. If a change occurs to the `spec.template` during a restart, the restart is canceled (by setting the `status.restartedAt` to the `spec.restartAt`) since the Rollout has to bring up a new stack.

Note: Unlike deployments, whose restarts are essentially a normal rolling upgrade which happened to be triggered by a timestamp in the pod spec annotation, Rollouts facilitates restarts by sequentially and individually terminating pods one by one. This design choice was made in order to allow a restart to occur even when a Rollout was in the middle of a long-running blue-green/canary update. However, some consequences of this are:

* Restarting a Rollout with a single replica will cause downtime since the Rollout would only have a single one Pod which would be brought down during the restart.
* Restarting a rollout occurs slower than a deployment's rolling update, since the sequential restarting of pods is more conservative than the maxSurge, maxUnavailable fields of a rolling update.


## Kubectl command

The Argo Rollouts kubectl plugin has a command for restarting Rollouts. Check it out [here](../generated/kubectl-argo-rollouts/kubectl-argo-rollouts_restart.md).

## Rescheduled Restarts

Users can reschedule a restart on their Rollout by setting the `.spec.restartAt` field to a time in the future. The controller only starts the restart after the current time is after the restartAt time. 
