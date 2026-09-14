# Argo Rollouts and Ambassador Quick Start

This tutorial will walk you through the process of configuring Argo Rollouts to work with Ambassador to facilitate canary releases. All files used in this guide are available in the [examples](https://github.com/argoproj/argo-rollouts/blob/master/examples/ambassador) directory of this repository.

## Requirements

- Kubernetes cluster
- Argo-Rollouts installed in the cluster

---
**Note**
If using Ambassador Edge Stack or Emissary-ingress 2.0+, you will need to install Argo-Rollouts version v1.1+, and you will need to supply `--ambassador-api-version getambassador.io/v3alpha1` to your `argo-rollouts` deployment.
---

## 1. Install and configure Ambassador Edge Stack

If you don't have Ambassador in your cluster you can install it following the [Edge Stack documentation](https://www.getambassador.io/docs/latest/topics/install/).

By default, Edge Stack routes via Kubernetes services. For best performance with canaries, we recommend you use endpoint routing. Enable endpoint routing on your cluster by saving the following configuration in a file called `resolver.yaml`:

```
apiVersion: getambassador.io/v2
kind: KubernetesEndpointResolver
metadata:
  name: endpoint
```

Apply this configuration to your cluster: `kubectl apply -f resolver.yaml`.

## 2. Create the Kubernetes Services

We'll create two Kubernetes services, named `echo-stable` and `echo-canary`. Save this configuration to the file `echo-service.yaml`.

```
apiVersion: v1
kind: Service
metadata:
  labels:
    app: echo
  name: echo-stable
spec:
  type: ClusterIP
  ports:
  - name: http
    port: 80
    protocol: TCP
    targetPort: 8080
  selector:
    app: echo
---
apiVersion: v1
kind: Service
metadata:
  labels:
    app: echo
  name: echo-canary
spec:
  type: ClusterIP
  ports:
  - name: http
    port: 80
    protocol: TCP
    targetPort: 8080
  selector:
    app: echo 
```

We'll also create an Edge Stack route to the services. Save the following configuration to a file called `echo-mapping.yaml`.

```
apiVersion: getambassador.io/v2
kind:  Mapping
metadata:
  name:  echo
spec:
  prefix: /echo
  rewrite: /echo
  service: echo-stable:80
  resolver: endpoint
```

Apply both of these configurations to the Kubernetes cluster:

```
kubectl apply -f echo-service.yaml
kubectl apply -f echo-mapping.yaml
```

## 3. Deploy the Echo Service

Create a Rollout resource and save it to a file called `rollout.yaml`. Note the `trafficRouting` attribute, which tells Argo to use Ambassador Edge Stack for routing.

```
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: echo-rollout
spec:
  selector:
    matchLabels:
      app: echo
  template:
    metadata:
      labels:
        app: echo
    spec:
      containers:
        - image: hashicorp/http-echo
          args:
            - "-text=VERSION 1"
            - -listen=:8080
          imagePullPolicy: Always
          name: echo-v1
          ports:
            - containerPort: 8080
  strategy:
    canary:
      stableService: echo-stable
      canaryService: echo-canary
      trafficRouting:
        ambassador:
          mappings:
            - echo
      steps:
      - setWeight: 30
      - pause: {duration: 30s}
      - setWeight: 60
      - pause: {duration: 30s}
      - setWeight: 100
      - pause: {duration: 10}
```

Apply the rollout to your cluster `kubectl apply -f rollout.yaml`. Note that no canary rollout will occur, as this is the first version of the service being deployed. 

## 4. Test the service

We'll now test that this rollout works as expected.  Open a new terminal window. We'll use it to send requests to the cluster. Get the external IP address for Edge Stack:

```
export AMBASSADOR_LB_ENDPOINT=$(kubectl -n ambassador get svc ambassador -o "go-template={{range .status.loadBalancer.ingress}}{{or .ip .hostname}}{{end}}")
```

Send a request to the `echo` service:  

```
curl -Lk "https://$AMBASSADOR_LB_ENDPOINT/echo/"
```

You should get a response of "VERSION 1".

## 5. Rollout a new version 

It's time to rollout a new version of the service. Update the echo container in the `rollout.yaml` to display "VERSION 2":

```
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: echo-rollout
spec:
  selector:
    matchLabels:
      app: echo
  template:
    metadata:
      labels:
        app: echo
    spec:
      containers:
        - image: hashicorp/http-echo
          args:
            - "-text=VERSION 2"
            - -listen=:8080
          imagePullPolicy: Always
          name: echo-v1
          ports:
            - containerPort: 8080
  strategy:
    canary:
      stableService: echo-stable
      canaryService: echo-canary
      trafficRouting:
        ambassador:
          mappings:
            - echo
      steps:
      - setWeight: 30
      - pause: {duration: 30s}
      - setWeight: 60
      - pause: {duration: 30s}
      - setWeight: 100
      - pause: {duration: 10}
```

Apply the rollout to the cluster by typing `kubectl apply -f rollout.yaml`. This will rollout a version 2 of the service by routing 30% of traffic to the service for 30 seconds, followed by 60% of traffic for another 30 seconds.

You can monitor the status of your rollout at the command line:

```
kubectl argo rollouts get rollout echo-rollout --watch
```

Will display an output similar to the following:

```
Name:            echo-rollout
Namespace:       default
Status:          ॥ Paused
Message:         CanaryPauseStep
Strategy:        Canary
  Step:          1/6
  SetWeight:     30
  ActualWeight:  30
Images:          hashicorp/http-echo (canary, stable)
Replicas:
  Desired:       1
  Current:       2
  Updated:       1
  Ready:         2
  Available:     2

NAME                                      KIND        STATUS        AGE    INFO
⟳ echo-rollout                            Rollout     ॥ Paused      2d21h
├──# revision:3
│  └──⧉ echo-rollout-64fb847897           ReplicaSet  ✔ Healthy     2s     canary
│     └──□ echo-rollout-64fb847897-49sg6  Pod         ✔ Running     2s     ready:1/1
├──# revision:2
│  └──⧉ echo-rollout-578bfdb4b8           ReplicaSet  ✔ Healthy     3h5m   stable
│     └──□ echo-rollout-578bfdb4b8-86z6n  Pod         ✔ Running     3h5m   ready:1/1
└──# revision:1
   └──⧉ echo-rollout-948d9c9f9            ReplicaSet  • ScaledDown  2d21h
```

In your other terminal window, you can verify that the canary is progressing appropriately by sending requests in a loop:

```
while true; do curl -k https://$AMBASSADOR_LB_ENDPOINT/echo/; sleep 0.2; done
```

This will display a running list of responses from the service that will gradually transition from VERSION 1 strings to VERSION 2 strings.

For more details about the Ambassador and Argo-Rollouts integration, see the [Ambassador Argo documentation](https://www.getambassador.io/docs/argo/latest/quick-start/).
