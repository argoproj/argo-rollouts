# Traefik

You can use the [Traefik Proxy](https://traefik.io/traefik/) for traffic management with Argo Rollouts.

The [TraefikService](https://doc.traefik.io/traefik/routing/providers/kubernetes-crd/#kind-traefikservice) is the object that supports the ability for [weighted round robin load balancing](https://doc.traefik.io/traefik/routing/providers/kubernetes-crd/#weighted-round-robin) and [traffic mirroring](https://doc.traefik.io/traefik/routing/providers/kubernetes-crd/#mirroring) when using Traefik as ingress.

!!! note
    Traefik is also supported via the  [Argo Rollouts Gateway API plugin](https://github.com/argoproj-labs/rollouts-plugin-trafficrouter-gatewayapi/). 

## How to integrate TraefikService with Argo Rollouts using it as weighted round robin load balancer

First, we need to create the TraefikService object using its ability for weighted round robin load balancing.

```yaml
apiVersion: traefik.containo.us/v1alpha1
kind: TraefikService
metadata:
  name: traefik-service
spec:
  weighted:
    services:
      - name: stable-rollout # k8s service name that you need to create for stable application version
        port: 80
      - name: canary-rollout # k8s service name that you need to create for new application version
        port: 80
```

Notice, we don't specify the `weight` field. It is necessary to be synced with ArgoCD. If we specify this field and Argo Rollouts controller changes it, then the ArgoCD controller will notice it and will show that this resource is out of sync (if you are using Argo CD to manage your Rollout).

Secondly, we need to create the Argo Rollouts object.

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: rollouts-demo
spec:
  replicas: 5
  strategy:
    canary:
      canaryService: canary-rollout
      stableService: stable-rollout
      trafficRouting:
        traefik:
          weightedTraefikServiceName: traefik-service # specify traefikService resource name that we have created before
      steps:
      - setWeight: 30
      - pause: {}
      - setWeight: 40
      - pause: {duration: 10}
      - setWeight: 60
      - pause: {duration: 10}
      - setWeight: 80
      - pause: {duration: 10}
  ...
```


