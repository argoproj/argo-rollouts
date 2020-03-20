# AWS Application Load Balancer (ALB)

The [AWS ALB Ingress Controller](https://kubernetes-sigs.github.io/aws-alb-ingress-controller/) enables traffic management through an Ingress object that configuring an ALB that routes traffic proportionally to different services. 

The ALB consists of a listener and rules with actions. Listeners define how traffic from client comes in, and rules define how to handle those requests with various actions. One action allows users to forward traffic to multiple TargetGroups (with each being defined as a Kubernetes service) You can read more about ALB concepts [here](https://docs.aws.amazon.com/elasticloadbalancing/latest/application/introduction.html).

An ALB Ingress defines a desired ALB with listener and rules through its annotations and spec. The ALB Ingress controller honors an annotation on an Ingress called `alb.ingress.kubernetes.io/actions.<service-name>` that allows users to define the actions of a service listed in the Ingress with a "use-annotations" value for the ports. Below is an example of an ingress:

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
    kubernetes.io/ingress.class: aws-alb
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
There are a couple of required fields in a Rollout to send split traffic between versions using ALB ingresses. Below is an example of a Rollout with those fields:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
spec:
  ...
  strategy:
    canary:
      canaryService: canary-service # required
      stableService: stable-service  # required
      trafficRouting:
        alb:
           ingress: ingress  # required
           annotationPrefix: custom.alb.ingress.kubernetes.io # optional
```

The ingress field is a reference to an Ingress in the same namespace of the Rollout. The Rollout requires this Ingress to modify the ALB to route traffic to the stable and canary Services. Within the Ingress, looks for the stableService within the Ingress's rules and adds an action annotation for that the action. As the Rollout progresses through the Canary steps, the controller updates the Ingress's action annotation to reflect the desired state of the Rollout enabling traffic splitting between two different versions.

Since the ALB Ingress controller allows users to configure the annotation prefix used by the Ingress controller, Rollouts can specify the optional `annotationPrefix` field. The Ingress uses that prefix instead of the default `alb.ingress.kubernetes.io` if the field set.

The Rollout adds another annotation called `rollouts.argoproj.io/managed-alb-actions` to the Ingress to help the controller manage the Ingresses. This annotation indicates which actions are being managed by Rollout objects (since multiple Rollouts can reference one Ingress). If a Rollout is deleted, the Argo Rollouts controller uses this annotation to see that this action is no longer managed, and it is reset to only the stable service with 100 weight.
