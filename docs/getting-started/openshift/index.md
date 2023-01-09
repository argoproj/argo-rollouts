# Getting Started - OpenShift Routing

This guide covers how Argo Rollouts integrates with [OpenShift Routing](https://docs.openshift.com/container-platform/4.7/networking/routes/route-configuration.html) for traffic management. This guide builds upon the [getting started guide](../../getting-started.md).

## Requirements
- OpenShift cluster with support for routes installed
- Argo Rollouts installed (see [install guide](../../installation.md))

!!! tip
    Make sure you are either switched to the correct namespace or you specify the namespace to be used in the following examples. It is assumed you are working on the `argo-rollouts` namespace from the installation guide.

## 1. Create Kubernetes Services

The following will create two service resources named `stable-service` and `canary-service`:

```yaml
apiVersion: v1
kind: Service
metadata:
  labels:
    app: rollouts-demo
  name: stable-service
spec:
  ports:
  - port: 80
    targetPort: http
    protocol: TCP
    name: http
  selector:
    app: rollouts-demo
---
apiVersion: v1
kind: Service
metadata:
  labels:
    app: rollouts-demo
  name: canary-service
spec:
  ports:
  - port: 80
    targetPort: http
    protocol: TCP
    name: http
  selector:
    app: rollouts-demo 
```
```shell
oc apply -f https://raw.githubusercontent.com/argoproj/argo-rollouts/master/docs/getting-started/openshift/services.yaml
```

## 2. Create OpenShift Route

Creation of a new route is easy through the OpenShift CLI tool:
```shell
oc expose service stable-service --name=main-route
```

Alternatively, create and apply a route of your own:
```yaml
apiVersion: route.openshift.io/v1
kind: Route
metadata:
  labels:
    app: rollouts-demo
  name: main-route
spec:
  port:
    targetPort: http
  to:
    kind: Service
    name: stable-service
    weight: 100
```
```shell
oc apply -f https://raw.githubusercontent.com/argoproj/argo-rollouts/master/docs/getting-started/openshift/route.yaml
```

In both cases, the new route is redirecting all traffic to the stable service.
 
## 3. Create Argo Rollout

You must define at least one route under the routes field:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: rollouts-demo
spec:
  replicas: 5
  strategy:
    canary:
      # Inserting traffic management here
      trafficRouting:
        openshift:
          routes:
          - main-route # Enter name of route
      canaryService: canary-service
      stableService: stable-service
      # Rest is same as getting started guide
      steps:
      - setWeight: 20
      - pause: {}
      - setWeight: 40
      - pause: {duration: 10}
      - setWeight: 60
      - pause: {duration: 10}
      - setWeight: 80
      - pause: {duration: 10}
  revisionHistoryLimit: 2
  selector:
    matchLabels:
      app: rollouts-demo
  template:
    metadata:
      labels:
        app: rollouts-demo
    spec:
      containers:
      - name: rollouts-demo
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
```shell
oc apply -f https://raw.githubusercontent.com/argoproj/argo-rollouts/master/docs/getting-started/openshift/rollout.yaml
```
Watch the rollout through the web [dashboard](../../dashboard.md).

Or use the CLI tool:
```shell
oc argo rollouts get rollout rollouts-demo -w
```

You should see that 5 pods are deployed with version 1 of the rollouts-demo container under the service `stable-service`. Since this is the first version, the rollout is in a finished state.

## 4. Update Rollout

In the dashboard, change the container image in the top right to `argoproj/rollouts-demo:yellow`.

Or use the CLI tool:
```shell
oc argo rollouts set image rollouts-demo \
  rollouts-demo=argoproj/rollouts-demo:yellow
```

The rollout should begin as normal, changing the traffic weight of the canary service to 20% and the stable service to 80%. This can be confirmed through the following command:

```shell
oc describe route main-route
```

## 5. Test Rollout

Get the route URL through the CLI:
```shell
oc get route main-route
```
Access the URL to see the live changes to the rollout.
!!! tip
    Your browser may not be showing the traffic shift between different versions once the rollout is promoted. This is because OpenShift have stickyness enabled by default. To solve this, either disable and clear all cookies for the route URL through your browser settings, or see the [OpenShift documentation](https://docs.openshift.com/container-platform/4.11/networking/routes/route-configuration.html#nw-using-cookies-keep-route-statefulness_route-configuration) on how to disable stickyness.

The rollout can then be promoted through the CLI or web interface, and the resulting changes should be observable through the route URL.