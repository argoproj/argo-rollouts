# Getting Started - Multiple Providers (Service Mesh Interface and NGiNX Ingress)

!!! important
    Available since v1.2

This guide covers how Argo Rollouts integrates with multiple TrafficRoutings, using
[Linkerd](https://linkerd.io) and 
[NGINX Ingress Controller](https://github.com/kubernetes/ingress-nginx) for traffic shaping, but you
should be able to produce any other combination between the existing trafficRouting options.

This guide builds upon the concepts of the [basic getting started guide](../../getting-started.md), 
[NGINX Guide](getting-started/nginx/index.md), and [SMI Guide](getting-started/smi/index.md).

## Requirements
- Kubernetes cluster with Linkerd installed
- Kubernetes cluster with NGINX ingress controller installed and part of the mesh

!!! tip
    See the [environment setup guide for linkerd](../setup/index.md#linkerd-setup)
    on how to setup a local minikube environment with linkerd and nginx.

## 1. Deploy the Rollout, Services, and Ingress

When SMI is used as one the traffic routers, the Rollout canary strategy must define
the following mandatory fields:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: rollouts-demo
spec:
  strategy:
    canary:
      # Reference to a Service which the controller will update to point to the canary ReplicaSet
      canaryService: rollouts-demo-canary
      # Reference to a Service which the controller will update to point to the stable ReplicaSet
      stableService: rollouts-demo-stable
      trafficRouting:
        smi: {}
```
When NGINX Ingress is used as the traffic router, the Rollout canary strategy must define
the following mandatory fields:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: rollouts-demo
spec:
  strategy:
    canary:
      # Reference to a Service which the controller will update to point to the canary ReplicaSet
      canaryService: rollouts-demo-canary
      # Reference to a Service which the controller will update to point to the stable ReplicaSet
      stableService: rollouts-demo-stable
      trafficRouting:
        nginx:
          # Reference to an Ingress which has a rule pointing to the stable service (e.g. rollouts-demo-stable)
          # This ingress will be cloned with a new name, in order to achieve NGINX traffic splitting.
          stableIngress: rollouts-demo-stable
...
```

A combination of both should have comply with each TrafficRouting requirements, in this case:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: rollouts-demo
spec:
  strategy:
    canary:
      # Reference to a Service which the controller will update to point to the canary ReplicaSet
      canaryService: rollouts-demo-canary
      # Reference to a Service which the controller will update to point to the stable ReplicaSet
      stableService: rollouts-demo-stable
      trafficRouting:
        nginx:
          # Reference to an Ingress which has a rule pointing to the stable service (e.g. rollouts-demo-stable)
          # This ingress will be cloned with a new name, in order to achieve NGINX traffic splitting.
          stableIngress: rollouts-demo-stable
        smi: {}
```

The Ingress referenced in `canary.trafficRouting.nginx.stableIngress` is required to have a host
rule which has a backend targeting the Service referenced under `canary.stableService`.
In our example, that stable Service is named: `rollouts-demo-stable`:

```yaml
apiVersion: networking.k8s.io/v1beta1
kind: Ingress
metadata:
  name: rollouts-demo-stable
  annotations:
    kubernetes.io/ingress.class: nginx
spec:
  rules:
  - host: rollouts-demo.local
    http:
      paths:
      - path: /
        backend:
          # Reference to a Service name, also specified in the Rollout spec.strategy.canary.stableService field
          serviceName: rollouts-demo-stable
          servicePort: 80
```

Run the following commands to deploy:

* A Rollout with the Linkerd `linkerd.io/inject: enabled` annotation
* Two Services (stable and canary)
* An Ingress

```shell
kubectl apply -f https://raw.githubusercontent.com/argoproj/argo-rollouts/master/docs/getting-started/mixed/rollout.yaml
kubectl apply -f https://raw.githubusercontent.com/argoproj/argo-rollouts/master/docs/getting-started/mixed/services.yaml
kubectl apply -f https://raw.githubusercontent.com/argoproj/argo-rollouts/master/docs/getting-started/mixed/ingress.yaml
```

After applying the manifests you should see the following rollout, services, and ingress resources
in the cluster:

```shell
$ kubectl get ro
NAME            DESIRED   CURRENT   UP-TO-DATE   AVAILABLE
rollouts-demo   1         2         1            2

$ kubectl get svc
NAME                   TYPE        CLUSTER-IP       EXTERNAL-IP   PORT(S)   AGE
rollouts-demo-canary   ClusterIP   10.111.69.188    <none>        80/TCP    23m
rollouts-demo-stable   ClusterIP   10.109.175.248   <none>        80/TCP    23m

$ kubectl get ing
NAME                   CLASS    HOSTS                 ADDRESS        PORTS   AGE
rollouts-demo-stable   <none>   rollouts-demo.local   192.168.64.2   80      23m
```

You should also see a TrafficSplit resource which is created automatically and owned by the rollout:

```
$ kubectl get trafficsplit
NAME            SERVICE
rollouts-demo   rollouts-demo-stable
```

When inspecting the generated TrafficSplit resource, the weights are automatically configured to
send 100% traffic to the `rollouts-demo-stable` service, and 0% traffic to the `rollouts-demo-canary`.
These values will be updated during an update.

```yaml
apiVersion: split.smi-spec.io/v1alpha1
kind: TrafficSplit
metadata:
  name: rollouts-demo
  namespace: default
spec:
  backends:
  - service: rollouts-demo-canary
    weight: "0"
  - service: rollouts-demo-stable
    weight: "100"
  service: rollouts-demo-stable
```

You should also notice a second ingress created by the rollouts controller,
`rollouts-demo-rollouts-demo-stable-canary`. This ingress is the "canary ingress", which is a
clone of the user-managed Ingress referenced under `nginx.stableIngress`. It is used by nginx
ingress controller to achieve canary traffic splitting. The name of the generated ingress is
formulated using `<ROLLOUT-NAME>-<INGRESS-NAME>-canary`. More details on the second Ingress are
discussed in the following section.

## 2. Perform an update

Now perform an update the rollout by changing the image, and wait for it to reached the paused state.

```shell
kubectl argo rollouts set image rollouts-demo rollouts-demo=argoproj/rollouts-demo:yellow
kubectl argo rollouts get rollout rollouts-demo
```

![Rollout Paused](../nginx/paused-rollout-nginx.png)

At this point, both the canary and stable version of the Rollout are running, with 5% of the
traffic directed to the canary and 95% to the stable. When inspecting the TrafficSplit generated by 
the controller, we see that the weight has been updated to reflect the current `setWeight: 5` step of 
the canary deploy.

```yaml
apiVersion: split.smi-spec.io/v1alpha1
kind: TrafficSplit
metadata:
  name: rollouts-demo
  namespace: default
spec:
  backends:
  - service: rollouts-demo-canary
    weight: "5"
  - service: rollouts-demo-stable
    weight: "95"
  service: rollouts-demo-stable
```
When inspecting the rollout controller generated Ingress copy, we see that it has the following
changes over the original ingress:

1. Two additional
[NGINX specific canary annotations](https://kubernetes.github.io/ingress-nginx/user-guide/nginx-configuration/annotations/#canary)
are added to the annotations.
2. The Ingress rules will have an rule which points the backend to the *canary* service.


```yaml
apiVersion: extensions/v1beta1
kind: Ingress
metadata:
  name: rollouts-demo-rollouts-demo-stable-canary
  annotations:
    kubernetes.io/ingress.class: nginx
    nginx.ingress.kubernetes.io/canary: "true"
    nginx.ingress.kubernetes.io/canary-weight: "5"
spec:
  rules:
  - host: rollouts-demo.local
    http:
      paths:
      - backend:
          serviceName: rollouts-demo-canary
          servicePort: 80
```

As the Rollout progresses through steps, the weights in the TrafficSplit and Ingress resource will be adjusted
to match the current setWeight of the steps.
