# Restarting Rollout Pods

For various reasons, applications often need to be restarted, e.g. for hygiene purposes or to force
startup logic to occur again such as reloading of a modified Secret. In these scenarios, it is
undesirable to go through an entire blue-green or canary update process. Argo Rollouts supports
the ability to restart all of its Pods by performing a rolling recreate of all the Pods in a Rollout
while skipping the regular BlueGreen or Canary update strategy.

## How it works

A rollout can be restarted via the kubectl plugin, using the
[restart command](../generated/kubectl-argo-rollouts/kubectl-argo-rollouts_restart.md):

```shell
kubectl-argo-rollouts restart ROLLOUT
```

Alternatively, if Rollouts is used with Argo CD, the there is a bundled "restart" action which can
be performed via the Argo CD UI or CLI:

```shell
argocd app actions run my-app restart --kind Rollout --resource-name my-rollout
```

Both of these mechanisms updates the Rollout's `.spec.restartAt` to the current time in the
form of a [RFC 3339 formatted](https://tools.ietf.org/html/rfc3339) UTC string
(e.g. 2020-03-30T21:19:35Z), which indicates to the Rollout controller that all of a Rollout's
Pods should have been created after this timestamp.

During a restart, the controller iterates through each ReplicaSet to see if all the Pods have a 
creation timestamp which is newer than the `restartAt` time. For every pod older than the
`restartAt` timestamp, the Pod will be evicted, allowing the ReplicaSet to replace the pod with a
recreated one.

To prevent too many Pods from restarting at once, the controller limits itself to deleting up to 
`maxUnavailable` Pods at a time. Secondly, since pods are evicted
and not deleted, the restart process will honor any PodDisruptionBudgets which are in place.
The controller restarts ReplicaSets in the following order:
  1. stable ReplicaSet
  2. current ReplicaSet
  3. all other ReplicaSets beginning with the oldest
  
If a Rollout's pod template spec (`spec.template`) is modified in the middle of a restart, the
restart is canceled, and the normal blue-green or canary update will occur.

Note: Unlike deployments, where a "restart" is nothing but a normal rolling upgrade that happened to
be triggered by a timestamp in the pod spec annotation, Argo Rollouts facilitates restarts by
terminating pods and allowing the existing ReplicaSet to replace the terminated pods. This design
choice was made in order to allow a restart to occur even when a Rollout was in the middle of a
long-running blue-green/canary update (e.g. a paused canary). However, some consequences of this are:

* Restarting a Rollout which has a single replica will cause downtime since Argo Rollouts needs to
  terminate the pod in order to replace it.
* Restarting a rollout will be slower than a deployment's rolling update, since maxSurge is not
  used to bring up newer pods faster.
* maxUnavailable will be used to restart multiple pods at a time (starting in v0.10). But if
  maxUnavailable pods is 0, the controller will still restart pods one at a time.

## Scheduled Restarts

Users can schedule a restart on their Rollout by setting the `.spec.restartAt` field to a time in
the future. The controller only starts the restart after the current time is after the restartAt
time. 
