# Gateway API

You can use the [Gateway API](https://gateway-api.sigs.k8s.io/) for traffic management with Argo Rollouts.

Gateway API is an open source project managed by the [SIG-NETWORK](https://github.com/kubernetes/community/tree/master/sig-network) community. It is a collection of resources that model service networking in Kubernetes.

## How to integrate Gateway API with Argo Rollouts

1. Enable Gateway Provider and create Gateway entrypoint
2. Create GatewayClass and Gateway resources
3. Create cluster entrypoint and map it with our Gateway entrypoint
4. Create HTTPRoute
5. Create canary and stable services
6. Create argo-rollouts resources

We will go through all these steps together with an example Traefik

### Enable Gateway Provider and create Gateway entrypoint

Every contoller has its own instruction how we need to enable Gateway API provider. I will follow to the instructions of [Traefik controller](https://doc.traefik.io/traefik/providers/kubernetes-gateway/)

1. Register [Gateway APY CRD with v1alpha2 version](https://gateway-api.sigs.k8s.io/v1alpha2/guides/getting-started/)

```
kubectl apply -k "github.com/kubernetes-sigs/gateway-api/config/crd?ref=v0.4.3"
```

2. Create the same deployment resource

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: traefik
spec:
  replicas: 1
  selector:
    matchLabels:
      app: argo-rollouts-traefik-lb
  template:
    metadata:
      labels:
        app: argo-rollouts-traefik-lb
    spec:
      serviceAccountName: traefik-controller
      containers:
        - name: traefik
          image: traefik:v2.6
          args:
            - --entrypoints.web.address=:80
            - --experimental.kubernetesgateway
            - --providers.kubernetesgateway
          ports:
            - name: web
              containerPort: 80 # entrypoint for our gateway
```

3. Create the same ServiceAccount

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: traefik-controller
```

4. Create Cluster Role resource with needing permissions for Gateway API provider

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  creationTimestamp: null
  name: traefik-controller-role
  namespace: aws-local-runtime
rules:
  - apiGroups:
      - "*"
    resources:
      - "*"
    verbs:
      - "*"
```

5. Create Cluster Role Binding

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: traefik-admin
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: traefik-controller-role
subjects:
  - namespace: default
    kind: ServiceAccount
    name: traefik-controller
```

### Create GatewayClass and Gateway resources

After we enable Gateway API provider in our controller we can create GatewayClass and Gateway:

- GatewayClass

```yaml
apiVersion: gateway.networking.k8s.io/v1alpha2
kind: GatewayClass
metadata:
  name: argo-rollouts-gateway-class
spec:
  controllerName: traefik.io/gateway-controller
```

- Gateway

```yaml
apiVersion: gateway.networking.k8s.io/v1alpha2
kind: Gateway
metadata:
  name: argo-rollouts-gateway
spec:
  gatewayClassName: argo-rollouts-gateway-class
  listeners:
    - protocol: HTTP
      name: web
      port: 80 # one of Gateway entrypoint that we created at 1 step
```

### Create cluster entrypoint and map it with our Gateway entrypoint

In different controllers entry points can be created differently. For Traefik controller we can create entry point like this:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: argo-rollouts-traefik-lb
spec:
  type: LoadBalancer
  selector:
    app: argo-rollouts-traefik-lb # selector of Gateway provider(step 1)
  ports:
    - protocol: TCP
      port: 8080
      targetPort: web # map with Gateway entrypoint
      name: web
```

### Create HTTPRoute

Create HTTPRoute and connect to the created Gateway resource

```yaml
apiVersion: gateway.networking.k8s.io/v1alpha2
kind: HTTPRoute
metadata:
  name: argo-rollouts-http-route
spec:
  parentRefs:
    - name: argo-rollouts-gateway
  rules:
    - backendRefs:
        - name: argo-rollouts-stable-service
          port: 80
        - name: argo-rollouts-canary-service
          port: 80
```

### Create canary and stable services

- Canary service

```yaml
apiVersion: v1
kind: Service
metadata:
  name: argo-rollouts-canary-service
spec:
  ports:
    - port: 80
      targetPort: http
      protocol: TCP
      name: http
  selector:
    app: rollouts-demo
```

- Stable service

```yaml
apiVersion: v1
kind: Service
metadata:
  name: argo-rollouts-stable-service
spec:
  ports:
    - port: 80
      targetPort: http
      protocol: TCP
      name: http
  selector:
    app: rollouts-demo
```

### Create argo-rollouts resources

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: rollouts-demo
spec:
  replicas: 5
  strategy:
    canary:
      canaryService: argo-rollouts-canary-service # our created canary service
      stableService: argo-rollouts-stable-service # our created stable service
      trafficRouting:
        plugin:
          gatewayAPI:
            httpRoute: argo-rollouts-http-route # our created httproute
      steps:
        - setWeight: 30
        - pause: {}
        - setWeight: 40
        - pause: { duration: 10 }
        - setWeight: 60
        - pause: { duration: 10 }
        - setWeight: 80
        - pause: { duration: 10 }
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
          image: argoproj/rollouts-demo:red
          ports:
            - name: http
              containerPort: 8080
              protocol: TCP
          resources:
            requests:
              memory: 32Mi
              cpu: 5m
```
