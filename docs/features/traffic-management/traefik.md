# Traefik

The [TraefikService](https://doc.traefik.io/traefik/routing/providers/kubernetes-crd/#kind-traefikservice) is the object supports the ability for [weighted round robin load balancing](https://doc.traefik.io/traefik/routing/providers/kubernetes-crd/#weighted-round-robin) and (traffic mirroring)[https://doc.traefik.io/traefik/routing/providers/kubernetes-crd/#mirroring] when using Traefik as ingress.

## How to integrate TraefikService with Argo Rollouts using it as weighted round robin load balancer

Firstly, we need to create the TraefikService object using its ability for weighted round robin load balancing.

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

Notice, we don't specify the `weight` field. It is necessary to be synced with ArgoCD. If we specify this field and Argo Rollouts controller will change it, ArgoCD controller will notice it and will show that this resource is out of sync.

Secondly, we need to create the Argo Rollouts controller.

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

## How to integrate TraefikService with Argo Rollouts using it as traffic mirror

Firstly, we also need to create the TraefikService object but using its ability for traffic mirroring.

```yaml
apiVersion: traefik.containo.us/v1alpha1
kind: TraefikService
metadata:
  name: traefik-service
spec:
  mirroring:
    name: some-service
    port: 80
    mirrors:
      - name: stable-rollout # k8s service name that you need to create for stable application version
        port: 80
      - name: canary-rollout # k8s service name that you need to create for new application version
        port: 80
```

Notice, we don't specify the `percent` field. It is necessary to be synced with ArgoCD. If we specify this field and Argo Rollouts controller will change it, ArgoCD controller will notice it and will show that this resource is out of sync.

Secondly, we need to create the Argo Rollouts controller.

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
          mirrorTraefikServiceName: traefik-service # specify traefikService resource name that we have created before
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
