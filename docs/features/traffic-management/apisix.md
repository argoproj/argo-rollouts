# Apache APISIX

You can use the [Apache APISIX](https://apisix.apache.org/) and [Apache APISIX Ingress Controller](https://apisix.apache.org/docs/ingress-controller/getting-started/) for traffic management with Argo Rollouts.

The [ApisixRoute](https://apisix.apache.org/docs/ingress-controller/concepts/apisix_route/) is the object that supports the ability for [weighted round robin load balancing](https://apisix.apache.org/docs/ingress-controller/concepts/apisix_route/#weight-based-traffic-split)  when using Apache APISIX Ingress Controller as ingress.

This guide shows you how to integrate ApisixRoute with Argo Rollouts using it as weighted round robin load balancer

## Prerequisites

Argo Rollouts requires  Apache APISIX v2.15 or newer and Apache APISIX Ingress Controller v1.5.0 or newer.

Install Apache APISIX and Apache APISIX Ingress Controller with Helm v3:

```bash
helm repo add apisix https://charts.apiseven.com
kubectl create ns apisix

helm upgrade -i apisix apisix/apisix --version=0.11.3 \
--namespace apisix \
--set ingress-controller.enabled=true \
--set ingress-controller.config.apisix.serviceNamespace=apisix
```

## Bootstrap

First, we need to create the ApisixRoute object using its ability for weighted round robin load balancing.

```yaml
apiVersion: apisix.apache.org/v2
kind: ApisixRoute
metadata:
  name: rollouts-apisix-route
spec:
  http:
    - name: rollouts-apisix
      match:
        paths:
          - /*
        hosts:
          - rollouts-demo.apisix.local
      backends:
        - serviceName: rollout-apisix-canary-stable
          servicePort: 80
        - serviceName: rollout-apisix-canary-canary
          servicePort: 80
```

```bash
kubectl apply -f https://raw.githubusercontent.com/argoproj/argo-rollouts/master/examples/apisix/route.yaml
```

Notice, we don't specify the `weight` field. It is necessary to be synced with ArgoCD. If we specify this field and Argo Rollouts controller changes it, then the ArgoCD controller will notice it and will show that this resource is out of sync (if you are using Argo CD to manage your Rollout).

Secondly, we need to create the Argo Rollouts object.

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: rollout-apisix-canary
spec:
  replicas: 5
  strategy:
    canary:
      canaryService: rollout-apisix-canary-canary
      stableService: rollout-apisix-canary-stable
      trafficRouting:
        managedRoutes:
          - name: set-header
        apisix:
          route:
            name: rollouts-apisix-route
            rules:
              - rollouts-apisix
      steps:
        - setCanaryScale:
            replicas: 1
          setHeaderRoute:
            match:
              - headerName: trace
                headerValue:
                  exact: debug
            name: set-header
        - setWeight: 20
        - pause: {}
        - setWeight: 40
        - pause:
            duration: 15
        - setWeight: 60
        - pause:
            duration: 15
        - setWeight: 80
        - pause:
            duration: 15
  revisionHistoryLimit: 2
  selector:
    matchLabels:
      app: rollout-apisix-canary
  template:
    metadata:
      labels:
        app: rollout-apisix-canary
    spec:
      containers:
        - name: rollout-apisix-canary
          image: argoproj/rollouts-demo:blue
          ports:
            - name: http
              containerPort: 8080
              protocol: TCP
          resources:
            requests:
              memory: 32Mi
              cpu: 5m
```

```bash
kubectl apply -f https://raw.githubusercontent.com/argoproj/argo-rollouts/master/examples/apisix/rollout.yaml
```

Finally, we need to create the services for the Argo Rollouts object.

```yaml
apiVersion: v1
kind: Service
metadata:
  name: rollout-apisix-canary-canary
spec:
  ports:
    - port: 80
      targetPort: http
      protocol: TCP
      name: http
  selector:
    app: rollout-apisix-canary
    # This selector will be updated with the pod-template-hash of the canary ReplicaSet. e.g.:
    # rollouts-pod-template-hash: 7bf84f9696
---
apiVersion: v1
kind: Service
metadata:
  name: rollout-apisix-canary-stable
spec:
  ports:
    - port: 80
      targetPort: http
      protocol: TCP
      name: http
  selector:
    app: rollout-apisix-canary
    # This selector will be updated with the pod-template-hash of the stable ReplicaSet. e.g.:
    # rollouts-pod-template-hash: 789746c88d
```

```bash
kubectl apply -f https://raw.githubusercontent.com/argoproj/argo-rollouts/master/examples/apisix/services.yaml
```

Initial creations of any Rollout will immediately scale up the replicas to 100% (skipping any canary upgrade steps, analysis, etc...) since there was no upgrade that occurred.

The Argo Rollouts kubectl plugin allows you to visualize the Rollout, its related resources (ReplicaSets, Pods, AnalysisRuns), and presents live state changes as they occur. To watch the rollout as it deploys, run the get rollout --watch command from plugin:

```bash
kubectl argo rollouts get rollout rollout-apisix-canary --watch
```

## Updating a Rollout

Next it is time to perform an update. Just as with Deployments, any change to the Pod template field (`spec.template`) results in a new version (i.e. ReplicaSet) to be deployed. Updating a Rollout involves modifying the rollout spec, typically changing the container image field with a new version, and then running  `kubectl apply` against the new manifest. As a convenience, the rollouts plugin provides a `set image` command, which performs these steps against the live rollout object in-place. Run the following command to update the `rollout-apisix-canary` Rollout with the "yellow" version of the container:

```shell
kubectl argo rollouts set image rollout-apisix-canary \
  rollouts-demo=argoproj/rollouts-demo:yellow
```

During a rollout update, the controller will progress through the steps defined in the Rollout's update strategy. The example rollout sets a 20% traffic weight to the canary, and pauses the rollout indefinitely until user action is taken to unpause/promote the rollout.

You can check ApisixRoute's backend weights by the following command
```bash
kubectl describe apisixroute rollouts-apisix-route

......
Spec:
  Http:
    Backends:
      Service Name:  rollout-apisix-canary-stable
      Service Port:  80
      Weight:        80
      Service Name:  rollout-apisix-canary-canary
      Service Port:  80
      Weight:        20
......
```
The `rollout-apisix-canary-canary` service gets 20% traffic through the Apache APISIX.

You can check SetHeader ApisixRoute's match by the following command
```bash
kubectl describe apisixroute set-header

......
Spec:
  Http:
    Backends:
      Service Name:  rollout-apisix-canary-canary
      Service Port:  80
      Weight:        100
    Match:
      Exprs:
        Op:  Equal
        Subject:
          Name:   trace
          Scope:  Header
        Value:    debug
......
```
