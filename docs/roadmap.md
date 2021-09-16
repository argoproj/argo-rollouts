# Roadmap

The item listed here are proposed items for Argo Rollouts and are subject to change. To see where items
may fall into releases, visit the [github milestones](https://github.com/argoproj/argo-rollouts/milestones)
and notice if the item appears in the milestone description.

- [Roadmap](#roadmap)
    - [Weight Verification](#weight-verification)
    - [Istio Canary using DestinationRule subsets](#istio-canary-using-destinationrule-subsets)
    - [Webhook Notifications](#webhook-notifications)
    - [Rollback Window](#rollback-window)
    - [Header Based Routing](#header-based-routing)
    - [Shadow Traffic](#shadow-traffic)

# v1.0

## Weight Verification

[Issue #701](https://github.com/argoproj/argo-rollouts/issues/701)

When Argo Rollouts adjusts a canary weight, it currently assumes that the adjustment was made and
moves on to the next step. However, for some traffic routing providers, this change can take a long
time to take effect (or possibly never even made) since external factors may cause the change to
become delayed.

This proposal is to add verification to the traffic routers so that after a setWeight step, the
rollout controller could verify that the weight took effect before moving on to the next step. This
is especially important for the ALB ingress controller which are affected by things like rate
limiting, the ALB ingress controller not running, etc...


## Istio Canary using DestinationRule subsets

[Issue #617](https://github.com/argoproj/argo-rollouts/issues/617)

Currently, Rollouts supports only host-level traffic splitting using two Kubernetes Services.
For some use cases (e.g. east-west canarying intra-cluster), this pattern not desirable and traffic
splitting should be achieved using two
Istio [DestinationRule Subsets](https://istio.io/latest/docs/reference/config/networking/destination-rule/#Subset)
instead.


## Workload Referencing

[Issue #676](https://github.com/argoproj/argo-rollouts/issues/676)

Currently, the Rollout spec contains both the deployment strategy (e.g. blueGreen/canary),
as well as the pod template. This proposal is to support a way to reference the pod template
definition from another group/kind (e.g. a Deployment, PodTemplate) so that the rollout strategy
could be separated from the workload definition. This is motivated by the following use cases:

* CRDs (e.g. Rollouts) are not supported well in kustomize, and strategic merge patches simply 
  don't work as expected with a Rollout because lists will be replaced and not merged. By
  referencing a native Kubernetes kind, kustomize would work expectedly against the k8s native
  referenced object, which is the portion of the spec that users typically want to customize
  overlays against.
* During a migration from a Deployment to a Rollout, it has been inconvenient for users to duplicate
  the entire Deployment spec to a Rollout, and keeping them always in sync during the transition.
  By referencing the definition, we would be able to eliminate the possibility of pod template spec
  duplication.


```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: guestbook-canary
spec:
  replicas: 5
  workloadRef:
    apiVersion: apps/v1
    kind: Deployment
    name: guestbook
  strategy:
    canary:
      steps:
      - setWeight: 20
      - pause: {duration: 1h}
      - setWeight: 40
      - pause: {duration: 1h}
      - setWeight: 60
      - pause: {duration: 1h}
      - setWeight: 80
      - pause: {duration: 1h}
```

# v1.1+

## Webhook Notifications

[Issue #369](https://github.com/argoproj/argo-rollouts/issues/369)

When a rollout transitions state, such as an aborted rollout due to failed analysis, there is no mechanism to notify an external system about the failure. Instead, users must currently put in place something to monitor the rollout, and notice the condition to take action. Monitoring a rollout is not always an option, since it requires that the external system have access to the Kubernetes API server.

A webhook notification feature of Rollouts would allow a push-based model where the Rollout controller itself would push an event to an external system, in the form of a webhook/cloud event.

## Rollback Windows

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
