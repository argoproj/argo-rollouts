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

## How to integrate TraefikService with Argo Rollouts using it as weighted round robin load balancer with mirroring

[![Traefik mirror architecture](/docs/concepts-assets/traefik-mirror-architecture.svg)](/docs/concepts-assets/traefik-mirror-architecture.svg)

1. Install argo-rollouts controller
2. Install traefik controller
3. Configure traefik controller using IngressRoute - what resource it should use for what entry point. Entry point is simply listen port of pods where traefik controller works. Traefik controller has to use mirror TraefikService for your entry point
4. Configure mirror TraefikService. Set weighted TraefikService as main route and you can set for now only TraefikServices or nothing for mirrors. You need to create mirror TraefikService with the name that matches pattern: **argo-mirror-%name of main TraefikService%**
5. Create all needing mirrors
6. Configure Argo Rollouts manifest
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
        managedRoutes:
          - name: mirror-route
        traefik:
          weightedTraefikServiceName: traefik-service # specify "main" traefik service
      steps:
      - setMirrorroute:
          name: mirror-route
          percentage: 30
      - pause: {}
      - setWeight: 40
      - pause: {duration: 10}
      - setWeight: 60
      - pause: {duration: 10}
      - setWeight: 80
      - pause: {duration: 10}
  ...
```
When Argo Rollouts gets to the step of setMirrorRoute we simply add object to the list of mirrors
```yaml
mirrors:
  - name: <name field from setMirrorRoute>
    kind: TraefikService
    percent: <percentage field from setMirrorRoute>
```
When Argo Rollouts gets to the stage of delete mirrors, we take managedRoute list and find the objects which contains the needing name and delete them from the list of mirrors in TraefikService
