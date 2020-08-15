# AWS Application Load Balancer (ALB)

## Requirements
* ALB Ingress Controller v1.1.5 or greater

## Overview

[AWS ALB Ingress Controller](https://kubernetes-sigs.github.io/aws-alb-ingress-controller/) enables traffic management through an Ingress object which configures an ALB to route traffic to one or more
Kubernetes services. ALBs supports the ability to split traffic through the concept of [weighted target groups](https://aws.amazon.com/blogs/aws/new-application-load-balancer-simplifies-deployment-with-weighted-target-groups/). This feature is supported by the AWS ALB Ingress Controller through annotations made in the Ingress object to configure "actions".

ALBs are configured via listeners and rules with actions. Listeners define how traffic from client comes in, and rules define how to handle those requests with various actions. One action allows users to forward traffic to multiple TargetGroups (with each being defined as a Kubernetes service) You can read more about ALB concepts [here](https://docs.aws.amazon.com/elasticloadbalancing/latest/application/introduction.html).

An ALB Ingress defines a desired ALB with listener and rules through its annotations and spec. To leverage multiple target groups, ALB Ingress controller looks to an annotation on an Ingress called [`alb.ingress.kubernetes.io/actions.<service-name>`](https://kubernetes-sigs.github.io/aws-alb-ingress-controller/guide/ingress/annotation/#actions). To indicate that the action annotation should be used for an specific ingress rule, the value "use-annotations" is used as the port value in lieue of a named or numeric port. Below is an example of an ingress which splits traffic between two Kubernetes services, canary-service and stable-service, with a traffic weight of 80 and 20 respectively:

```yaml
apiVersion: extensions/v1beta1
kind: Ingress
metadata:
  annotations:
    alb.ingress.kubernetes.io/actions.stable-service: |
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
    kubernetes.io/ingress.class: alb
  name: ingress
spec:
  rules:
    - http:
        paths:
          - backend:
              serviceName: stable-service
              servicePort: use-annotation
            path: /*
```

This Ingress uses the `alb.ingress.kubernetes.io/actions.stable-service` annotation to define how to route traffic to the various services for the rule with the `stable-service` serviceName instead of sending traffic to the canary-service service. You can read more about these annotations on the official [documentation](https://kubernetes-sigs.github.io/aws-alb-ingress-controller/guide/ingress/annotation/#actions).

## Integration with Argo Rollouts
To configure a Rollout to split traffic between the canary and stable services during update via its ALB integration, the following fields should be specified:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
spec:
  strategy:
    canary:
      canaryService: canary-service  # required
      stableService: stable-service  # required
      trafficRouting:
        alb:
          ingress: ingress  # required
          servicePort: 443  # required
          rootService: # optional
          annotationPrefix: custom.alb.ingress.kubernetes.io # optional
```

The ingress field is a reference to an Ingress in the same namespace of the Rollout, and the servicePort field refers a port of a service. The Rollout requires the Ingress and servicePort to modify the ALB to route traffic to the stable and canary Services. Within the Ingress, looks for the stableService (or the optional rootService if specified) within the Ingress's rules and adds an action annotation for that the action. As the Rollout progresses through the Canary steps, the controller updates the Ingress's action annotation to reflect the desired state of the Rollout enabling traffic splitting between two different versions.

Since the ALB Ingress controller allows users to configure the annotation prefix used by the Ingress controller, Rollouts can specify the optional `annotationPrefix` field. The Ingress uses that prefix instead of the default `alb.ingress.kubernetes.io` if the field set.

The Rollout adds another annotation called `rollouts.argoproj.io/managed-alb-actions` to the Ingress to help the controller manage the Ingresses. This annotation indicates which actions are being managed by Rollout objects (since multiple Rollouts can reference one Ingress). If a Rollout is deleted, the Argo Rollouts controller uses this annotation to see that this action is no longer managed, and it is reset to only the stable service with 100 weight.

## Using Argo Rollouts with multiple ALB ingress controllers
As a default, the Argo Rollouts controller only operates on ingresses with the `kubernetes.io/ingress.class` annotation set to `alb`. A user can configure the controller to operate on Ingresses with different `kubernetes.io/ingress.class` values by specifying the `--alb-ingress-classes` flag. A user can list the `--alb-ingress-classes` flag multiple times if the Argo Rollouts controller should operate on multiple values. This may be desired when a cluster has multiple Ingress controllers that operate on different `kubernetes.io/ingress.class` values.

If the controller needs to operate on any Ingress without the `kubernetes.io/ingress.class` annotation, the flag can be specified with an empty string (e.g. `--alb-ingress-classes ''`).