# Roadmap

The item listed here are proposed items for Argo Rollouts and are subject to change. To see where items
may fall into releases, visit the [github milestones](https://github.com/argoproj/argo-rollouts/milestones)
and notice if the item appears in the milestone description.

- [Roadmap](#roadmap)
    - [Validation](#validation-continued)
    - [Webhook Notifications](#webhook-notifications)
    - [Ephemeral Canary Labels](#ephemeral-canary-labels)
    - [Rollback Window](#rollback-window)
    - [Header Based Routing](#header-based-routing)
    - [Shadow Traffic](#shadow-traffic)


## Validation (continued)

The v0.9 rollouts release added improved validation of user made errors in the Rollout spec, and now surfaces the reason for errors to the Rollout conditions and ultimately to tools like Argo CD.

Additional improvements regarding validation are being considered such as:
* a validating webhook to prevent invalid objects from entering the system.
* a linting command in the CLI to statically validate rollout objects.

## Webhook Notifications

[Issue #369](https://github.com/argoproj/argo-rollouts/issues/369)

When a rollout transitions state, such as an aborted rollout due to failed analysis, there is no mechanism to notify an external system about the failure. Instead, users must currently put in place something to monitor the rollout, and notice the condition to take action. Monitoring a rollout is not always an option, since it requires that the external system have access to the Kubernetes API server.

A webhook notification feature of Rollouts would allow a push-based model where the Rollout controller itself would push an event to an external system, in the form of a webhook/cloud event.

## Ephemeral Canary Labels

[Issue #445](https://github.com/argoproj/argo-rollouts/issues/455)

Currently the pods which are associated with a Rollout's canary and stable ReplicaSet, are not labeled with a predictable label. Instead they are labeled with a value under `rollouts-pod-template-hash`, which is a hashed value of the pod template spec. This value is always changing, and makes it very inconvenient to creating dashboards or formulate static queries against the canary/stable ReplicaSets, for the purposes of analysis.

This enhancement would allow user-defined labels to be attached to the canary/stable pods, as well as removed when the pods changes roles. For example, during an upgrade, the canary pods could be labeled with a `canary` label, which would be removed or relabeled as to `stable`, once the Rollout was successful.


## Rollback Window

[Issue #574](https://github.com/argoproj/argo-rollouts/issues/574)

Currently, when an older Rollout manifest is re-applied, the controller treats it the same as a spec change, and will execute the full list of steps, and perform analysis too. There are two exceptions to this rule:
1. the controller detects if it is moving back to a blue-green ReplicaSet which exists and is still scaled up (within its scaleDownDelay)
2. the controller detects it is moving back to the canary's "stable" ReplicaSet, and the upgrade had not yet completed.

It is often undesirable to re-run analysis and steps for a rollout, when the desired behavior is to rollback as soon as possible. To help with this, a rollback window feature would allow users a window indicate to the controller to 

## Header Based Routing

[Issue #474](https://github.com/argoproj/argo-rollouts/issues/474)

Users who are using Rollout with a service mesh, may want to leverage some of its more advanced features, such as routing traffic via headers instead of purely by percentage. Header based routing provides the ability to route traffic based on a header, instead of a percentage of traffic. This allows more flexibility when canarying, such as providing session stickiness, or only exposing a subset of users with a HTTP cookie or user-agent.

## Shadow Traffic

[Issue #474](https://github.com/argoproj/argo-rollouts/issues/474)

Some service meshes provide the ability to "shadow" live production traffic. A feature in rollouts could provide a canary step to shadow traffic to the canary stack, to see how it responds to the real-world data.
