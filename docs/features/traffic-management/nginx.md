# Nginx

NOTE: This is being implemented for the v0.7 version and everything described below is subject to change.

The [Nginx Ingress Controller](https://kubernetes.github.io/ingress-nginx/) enables traffic management through one or more Ingress objects to configure an Nginx deployment that routes traffic directly to pods. Each Nginx Ingress contains multiple annotations that modify the behavior of the Nginx Deployment. For traffic management between different versions of an application, the Nginx Ingress controller provides the capability to split traffic by introducing a second Ingress object (referred to as the canary Ingress) with some special annotations. Here are the canary specific annotations: 

- `nginx.ingress.kubernetes.io/canary` indicates that this Ingress is serving canary traffic
- `nginx.ingress.kubernetes.io/canary-weight` indicates what percentage of traffic to send to the canary.
- Other canary-specific annotations deal with routing traffic via headers or cookies. A future version of Argo Rollouts will contain this functionality.

 You can read more about these canary annotations on the official [documenentation page](https://kubernetes.github.io/ingress-nginx/user-guide/nginx-configuration/annotations/#canary). The canary Ingress ignores any other non-canary nginx annotations. Instead, it leverages the annotation settings from the primary Ingress.

## Integration with Argo Rollouts
There are a couple of required fields in a Rollout to send split traffic between versions using Nginx. Below is an example of a Rollout with those fields:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
spec:
  ...
  strategy:
    canary:
      canaryService: canary-service # required
      stableService: stable-service  # required
      networking:
        nginx:
           primaryIngress: primary-ingress  # required
           annotationPrefix: example.nginx.com/ # optional
```

The primary Ingress field is a reference to an Ingress in the same namespace of the Rollout. The Rollout requires the primary Ingress routes traffic to the stable ReplicaSet. The Rollout checks that condition by confirming the Ingress has a backend that matches the Rollout's stableService.

The controller routes traffic to the canary ReplicaSet by creating a second Ingress with the canary annotations. As the Rollout progresses through the Canary steps, the controller updates the canary Ingress's canary annotations to reflect the desired state of the Rollout enabling traffic splitting between two different versions.

Since the Nginx Ingress controller allows users to configure the annotation prefix used by the Ingress controller, Rollouts can specify the optional `annonationPrefix` field. The canary Ingress uses that prefix instead of the default `nginx.ingress.kubernetes.io` if the field set.