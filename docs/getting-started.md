# Getting Started

## Requirements
- Installed kubectl command-line tool
- Have a kubeconfig file (default location is ~/.kube/config).

## Install Argo Rollouts
Argo Rollouts can be installed at a cluster or namespace level. 

### Cluster-Level installation

```bash
kubectl create namespace argo-rollouts
kubectl apply -n argo-rollouts -f https://raw.githubusercontent.com/argoproj/argo-rollouts/stable/manifests/install.yaml
```

This will create a new namespace, `argo-rollouts`, where Argo Rollouts controller will live.

On GKE, you will need grant your account the ability to create new cluster roles:
    
```bash
kubectl create clusterrolebinding YOURNAME-cluster-admin-binding --clusterrole=cluster-admin --user=YOUREMAIL@gmail.com
```

### Namespace-Level Installation
```bash
kubectl apply -f https://raw.githubusercontent.com/argoproj/argo-rollouts/stable/manifests/namespace-install.yaml
```

## Converting Deployment to Rollout
Converting a Deployment to a Rollout simply is a core design principle of Argo Rollouts. There are two key changes:

1. Changing the `apiVersion` value to `argoproj.io/v1alpha1` and changing the `kind` value from `Deployment` to `Rollout`
1. Adding a new deployment strategy to the new Rollout. You can read up on the available strategies at [Argo Rollouts section](index.md)

Below is an example of a Rollout YAML using the Canary strategy.

```yaml
apiVersion: argoproj.io/v1alpha1 # Changed from apps/v1
kind: Rollout # Changed from Deployment
# ----- Everything below this comment is the same as a deployment -----
metadata:
  name: example-rollout
spec:
  replicas: 5
  selector:
    matchLabels:
      app: nginx
  template:
    metadata:
      labels:
        app: nginx
    spec:
      containers:
      - name: nginx
        image: nginx:1.15.4
        ports:
        - containerPort: 80
  minReadySeconds: 30
  revisionHistoryLimit: 3
  strategy:
  # ----- Everything above this comment are the same as a deployment -----
    canary: # A new field that used to provide configurable options for a Canary strategy
      steps:
      - setWeight: 20
      - pause: {}
```

## Updating the Rollout
The initial creation of the above Rollout will bring up all 5 replicas of the Pod Spec listed. Since the rollout was not in a stable state beforehand (as it was just created), the rollout will skip the steps listed in the `.spec.strategy.canary.steps` field to first become stable. Once the new ReplicaSet is healthy, updating any field in the `spec.template` will cause the rollout to create a new ReplicaSet and execute the steps in `spec.strategy.canary.steps` to transition to the new version.

To demonstrate this, we will update the rollout to use a new nginx image. You can either run `kubectl edit rollout example-rollout` and change the image from `nginx:1.15.4` to `nginx:1.15.5`, or run the following:

```bash
$ kubectl patch rollout example-rollout --type merge -p '{"spec": {"template": { "spec": { "containers": [{"name": "ngnix","image": "nginx:1.15.5"}]}}}}'
```

Once the patch is applied, you can watch the new replicaset came up as healthy by running 
```bash 
$ kubectl get replicaset -w -o wide
```
Once that replicaset is healthy, the rollout will enter a paused state by setting the `spec.paused` field to true, and setting the `.status.pauseStartedTime` to the current time.

## Unpausing the rollout
The rollout can be unpaused by running `kubectl edit rollout example-rollout` and setting the `spec.paused` field to `false` or the following
```bash
$ kubectl patch rollout example-rollout --type merge -p '{"spec": {"paused": false}}'
```

At this point, the Rollout has executed all the steps to transition to a new version. As a result, the new ReplicaSet is considered the new stable ReplicaSet, and the previous ReplicaSet will be scaled down. The Rollout will repeat this behavior if the Pod Spec Template is changed again.

## Going forward
Check out the [spec documentation](/spec) for more information on other available configuration for a rollout.
