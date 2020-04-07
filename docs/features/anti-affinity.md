# Anti Affinity

Depending on a cluster's configuration, a rollout can cause newly created pods to restart after ~10 minutes.

This behavior usually occurs when a cluster has its own dedicated instance group since a rollout has a greater effect on node-autoscaling.

For applications that cannot start up quickly or do not gracefully exit, this behavior can be problematic.

This behavior occurs because the node auto-scaler wants to scale down the extra capacity created to support a rollout running in double capacity.

Here is a visual representation of the issue:

Here is a rollout with 8 pods spread across 2 nodes. Each node can hold 6 pods.

When a new version is introduced, the total number of pods doubles. In this example, the total number of pods increases to 16.

Since each node can only hold 6 pods, the cluster autoscaler must increase the node count to 3 nodes to accomodate all 16 pods.

The resulting distribution of pods across nodes is shown here:


Once the rollout finishes progressing, the previous version is scaled down. This leaves the cluster over-provisioned, as shown below.


The node auto-scaler terminates the node and the pods are rescheduled on the remaining 2 nodes.


To prevent this behavior from happening, rollout can inject node anti-affinity to prevent new pods from sharing a node with the previous version's pods.

Anti-affinity is enabled by adding the antiAffinity flag to the Blue-Green or Canary strategy, as shown below.

Users have a choice between "re"