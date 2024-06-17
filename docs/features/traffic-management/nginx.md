# Nginx

The [Nginx Ingress Controller](https://kubernetes.github.io/ingress-nginx/) enables traffic management through one or more Ingress objects to configure an Nginx deployment that routes traffic directly to pods. Each Nginx Ingress contains multiple annotations that modify the behavior of the Nginx Deployment. For traffic management between different versions of an application, the Nginx Ingress controller provides the capability to split traffic by introducing a second Ingress object (referred to as the canary Ingress) with some special annotations. You can read more about these canary annotations on the official [canary annotations documentation page](https://kubernetes.github.io/ingress-nginx/user-guide/nginx-configuration/annotations/#canary). The canary Ingress ignores any other non-canary nginx annotations. Instead, it leverages the annotation settings from the primary Ingress.

The Rollout controller will always set the following two annotations on the canary Ingress (using your configured or the default `nginx.ingress.kubernetes.io` prefix):

- `canary: true` to indicate that this is the canary Ingress
- `canary-weight: <num>` to indicate what percentage of traffic to send to the canary. If all traffic is routed to the stable Service, this is set to `0`

You can provide additional annotations to add to the canary Ingress via the `additionalIngressAnnotations` field to enable features like routing by header or cookie.


## Integration with Argo Rollouts
There are a couple of required fields in a Rollout to send split traffic between versions using Nginx. Below is an example of a Rollout with those fields:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
spec:
  ...
  strategy:
    canary:
      canaryService: canary-service  # required
      stableService: stable-service  # required
      trafficRouting:
        nginx:
          # Either stableIngress or stableIngresses must be configured, but not both.
          stableIngress: primary-ingress
          stableIngresses:
            - primary-ingress
            - secondary-ingress
            - tertiary-ingress
          annotationPrefix: customingress.nginx.ingress.kubernetes.io # optional
          additionalIngressAnnotations:   # optional
            canary-by-header: X-Canary
            canary-by-header-value: iwantsit
```

The stable Ingress field is a reference to an Ingress in the same namespace of the Rollout. The Rollout requires the primary Ingress routes traffic to the stable Service. The Rollout checks that condition by confirming the Ingress has a backend that matches the Rollout's stableService.

The controller routes traffic to the canary Service by creating a second Ingress with the canary annotations. As the Rollout progresses through the Canary steps, the controller updates the canary Ingress's canary annotations to reflect the desired state of the Rollout enabling traffic splitting between two different versions.

Since the Nginx Ingress controller allows users to configure the annotation prefix used by the Ingress controller, Rollouts can specify the optional `annotationPrefix` field. The canary Ingress uses that prefix instead of the default `nginx.ingress.kubernetes.io` if the field set.


## Using Argo Rollouts with multiple NGINX ingress controllers per service
Starting with v1.5, argo rollouts supports multiple Nginx ingress controllers pointing at one service with canary deployments. If only one ingress controller is needed, utilize the existing key `stableIngress`. If multiple ingress controllers are needed (e.g., separating internal vs external traffic), use the key `stableIngresses` instead. It takes an array of string values that are the names of the ingress controllers. Canary steps are applied identically across all ingress controllers.


## Using Argo Rollouts with custom NGINX ingress controller names
As a default, the Argo Rollouts controller only operates on ingresses with the `kubernetes.io/ingress.class` annotation or `spec.ingressClassName` set to `nginx`. A user can configure the controller to operate on Ingresses with different class name by specifying the `--nginx-ingress-classes` flag. A user can list the `--nginx-ingress-classes` flag multiple times if the Argo Rollouts controller should operate on multiple values. This solves the case where a cluster has multiple Ingress controllers operating on different class values.

If the user would like the controller to operate on any Ingress without the `kubernetes.io/ingress.class` annotation or `spec.ingressClassName`, a user should add the following `--nginx-ingress-classes ''`.
