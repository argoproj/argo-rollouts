# AWS Load Balancer Controller (ALB)

## Requirements
* AWS Load Balancer Controller v1.1.5 or greater

## Overview

[AWS Load Balancer Controller](https://github.com/kubernetes-sigs/aws-load-balancer-controller) 
(also known as AWS ALB Ingress Controller) enables traffic management through an Ingress object,
which configures an AWS Application Load Balancer (ALB) to route traffic to one or more Kubernetes
services. ALBs provides advanced traffic splitting capability through the concept of
[weighted target groups](https://aws.amazon.com/blogs/aws/new-application-load-balancer-simplifies-deployment-with-weighted-target-groups/).
This feature is supported by the AWS Load Balancer Controller through annotations made to the
Ingress object to configure "actions."

## How it works

ALBs are configured via Listeners, and Rules which contain Actions. Listeners define how traffic
from a client comes in, and Rules define how to handle those requests with various Actions. One
type of Action allows users to forward traffic to multiple TargetGroups (with each being defined as
a Kubernetes service). You can read more about ALB concepts
[here](https://docs.aws.amazon.com/elasticloadbalancing/latest/application/introduction.html).

An Ingress which is managed by the AWS Load Balancer Controller, controls an ALB's Listener and
Rules through the Ingress' annotations and spec. In order to split traffic among multiple target
groups (e.g. different Kubernetes services), the AWS Load Balancer controller looks to a specific
"action" annotation on the Ingress,
[`alb.ingress.kubernetes.io/actions.<service-name>`](https://kubernetes-sigs.github.io/aws-load-balancer-controller/latest/guide/ingress/annotations/#actions).
This annotation is injected and updated automatically by a Rollout during an update according to
the desired traffic weights.

## Usage

To configure a Rollout to use the ALB integration and split traffic between the canary and stable 
services during updates, the Rollout should be configured with the following fields:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
...
spec:
  strategy:
    canary:
      # canaryService and stableService are references to Services which the Rollout will modify
      # to target the canary ReplicaSet and stable ReplicaSet respectively (required).
      canaryService: canary-service
      stableService: stable-service
      trafficRouting:
        alb:
          # The referenced ingress will be injected with a custom action annotation, directing
          # the AWS Load Balancer Controller to split traffic between the canary and stable
          # Service, according to the desired traffic weight (required).
          ingress: ingress
          # Reference to a Service that the Ingress must target in one of the rules (optional).
          # If omitted, uses canary.stableService.
          rootService: root-service
          # Service port is the port which the Service listens on (required).
          servicePort: 443
```

The referenced Ingress should be deployed with an ingress rule that matches the Rollout service:

```yaml
apiVersion: networking.k8s.io/v1beta1
kind: Ingress
metadata:
  name: ingress
  annotations:
    kubernetes.io/ingress.class: alb
spec:
  rules:
  - http:
      paths:
      - path: /*
        backend:
          # serviceName must match either: canary.trafficRouting.alb.rootService (if specified),
          # or canary.stableService (if rootService is omitted)
          serviceName: root-service
          # servicePort must be the value: use-annotation
          # This instructs AWS Load Balancer Controller to look to annotations on how to direct traffic
          servicePort: use-annotation
```

During an update, the rollout controller injects the `alb.ingress.kubernetes.io/actions.<SERVICE-NAME>`
annotation, containing a JSON payload understood by the AWS Load Balancer Controller, directing it
to split traffic between the `canaryService` and `stableService` according to the current canary weight. 

The following is an example of our example Ingress after the rollout has injected the custom action 
annotation that splits traffic between the canary-service and stable-service, with a traffic weight
of 80 and 20 respectively:

```yaml
apiVersion: networking.k8s.io/v1beta1
kind: Ingress
metadata:
  name: ingress
  annotations:
    kubernetes.io/ingress.class: alb
    alb.ingress.kubernetes.io/actions.root-service: |
      { 
        "Type":"forward",
        "ForwardConfig":{ 
          "TargetGroups":[ 
            { 
                "Weight":10,
                "ServiceName":"canary-service",
                "ServicePort":"80"
            },
            { 
                "Weight":90,
                "ServiceName":"stable-service",
                "ServicePort":"80"
            }
          ]
        }
      }
spec:
  rules:
  - http:
      paths:
      - path: /*
        backend:
          serviceName: root-service
          servicePort: use-annotation
```

!!! note

    Argo rollouts additionally injects an annotation, `rollouts.argoproj.io/managed-alb-actions`,
    to the Ingress for bookkeeping purposes. The annotation indicates which actions are being
    managed by the Rollout object (since multiple Rollouts can reference one Ingress). Upon a
    rollout deletion, the rollout controller looks to this annotation to understand that this action
    is no longer managed, and is reset to point only the stable service with 100 weight.


### rootService

By default, a rollout will inject the `alb.ingress.kubernetes.io/actions.<SERVICE-NAME>` annotation
using the service/action name specified under `spec.strategy.canary.stableService`. However, it may
be desirable to specify an explicit service/action name different from the `stableService`. For
example, [one pattern](/best-practices/#ingress-desiredstable-host-routes) is to use a single
Ingress containing three different rules to reach the canary, stable, and root service separately
(e.g. for testing purposes). In this case, you may want to specify a "root" service as the
service/action name instead of stable. To do so, reference a service under `rootService` under the
alb specification:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
spec:
  strategy:
    canary:
      canaryService: guestbook-canary
      stableService: guestbook-stable
      trafficRouting:
        alb:
          rootService: guestbook-root
...
```

### Weight verification

!!! note

    Since Argo Rollouts v1.0

When Argo Rollouts adjusts a canary weight by updating the Ingress annotation, it assumes that
the new weight immediately takes effect and moves on to the next step. However, due to external
factors (e.g. AWS rate limiting, AWS load balancer controller downtime) it is possible that the
ingress modification may take a long time to take effect (or possibly never even made). This is
potentially dangerous when the rollout completes its steps, it will scale down the old stack. If
the ALB Rules/Actions were still directing traffic to the old stack (because the weights never took
effect), then this would cause downtime to the service when the old stack was scaled down.

To mitigate this, the rollout controller has a feature to additionally *verify* the canary weight 
after a `setWeight` canary step. It accomplishes this by querying AWS LoadBalancer APIs directly,
to confirm that the Rules, Actions, and TargetGroups reflect the desire of Ingress annotation.
To enable ALB weight verification, add `--alb-verify-weight` flag to the rollout-controller flags:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: argo-rollouts
spec:
  template:
    spec:
      containers:
      - name: argo-rollouts
        args: [--alb-verify-weight]
```

For this feature to work, the argo-rollouts deployment requires the following AWS API permissions
under the [Elastic Load Balancing API](https://docs.aws.amazon.com/elasticloadbalancing/latest/APIReference/Welcome.html):

* `DescribeTargetGroups`
* `DescribeLoadBalancers`
* `DescribeListeners`
* `DescribeRules`
* `DescribeTags`

There are various ways of granting AWS privileges to the argo-rollouts pods, which is highly
dependent to your cluster's AWS environment, and out-of-scope of this documentation. Some solutions
include:

* AWS access and secret keys
* [kiam](https://github.com/uswitch/kiam)
* [kube2iam](https://github.com/jtblin/kube2iam)
* [EKS ServiceAccount IAM Roles](https://docs.aws.amazon.com/eks/latest/userguide/specify-service-account-role.html)


### Custom annotations-prefix

The AWS Load Balancer Controller allows users to customize the
[annotation prefix](https://kubernetes-sigs.github.io/aws-load-balancer-controller/latest/guide/ingress/annotations/#ingress-annotations)
used by the Ingress controller using a flag to the controller, `--annotations-prefix` (from the
default of `alb.ingress.kubernetes.io`). If your AWS Load Balancer Controller is customized to use
a different annotation prefix, `annotationPrefix` field should be specified such that the Ingress
object will be annotated in a manner understood by the cluster's aws load balancer controller.

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
spec:
  strategy:
    canary:
      trafficRouting:
        alb:
          annotationPrefix: custom.alb.ingress.kubernetes.io
```

### Custom kubernetes.io/ingress.class

By default, Argo Rollout will operates on Ingresses with the annotation:

```yaml
apiVersion: networking.k8s.io/v1beta1
kind: Ingress
metadata:
  annotations:
    kubernetes.io/ingress.class: alb
```

To configure the controller to operate on Ingresses with different `kubernetes.io/ingress.class`
values, the controller can specify a different value through the `--alb-ingress-classes` flag in
the controller command line arguments.


Note that the `--alb-ingress-classes` flag can be specified multiple times if the Argo Rollouts
controller should operate on multiple values. This may be desired when a cluster has multiple
Ingress controllers that operate on different `kubernetes.io/ingress.class` values.

If the controller needs to operate on any Ingress without the `kubernetes.io/ingress.class`
annotation, the flag can be specified with an empty string (e.g. `--alb-ingress-classes ''`).
