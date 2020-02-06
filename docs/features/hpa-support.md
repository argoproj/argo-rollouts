# Horizontal Pod Autoscaling
Horizontal Pod Autoscaling (HPA) automatically scales the number of pods in owned by a Kubernetes resource based on observed CPU utilization or user-configured metrics. In order to accomplish this behavior, HPA only supports resources with the scale endpoint enabled with a couple of required fields. The scale endpoint allows the HPA to understand the current state of a resource and modify the resource to scale it appropriately.  Argo Rollouts added support for the scale endpoint in the `0.3.0` release. After being modified by the HPA, the Argo Rollouts controller is responsible for reconciling that change in replicas. Since the strategies within a Rollout are very different, the Argo Rollouts controller handles the scale endpoint differently for various strategies. Below is the behavior for the different strategies:

## Blue Green
The HPA will scale rollouts using the `BlueGreen` strategy using the metrics from the ReplicaSet receiving traffic from the active service. When the HPA changes the replicas count, the Argo Rollouts controller will first scale up the ReplicaSet receiving traffic from the active service before ReplicaSet receiving traffic from the preview service. The controller will scale up the ReplicaSet receiving traffic from the preview service to prepare it for when the rollout switches the preview to active.  If there are no ReplicaSets receiving from the active service, the controller will use all the pods that match the base selector to determine scaling events. In that case, the controller will scale up the latest ReplicaSet to the new count and scale down the older ReplicaSets.

## Canary (ReplicaSet based)
The HPA will scale rollouts using the `Canary` Strategy using the metrics of all the ReplicasSets within the rollout. Since the Argo Rollouts controller does not control the service that sends traffic to those ReplicaSets, it assumes that all the ReplicaSets in the rollout are receiving traffic.

## Example

Below is an example of a Horizontal Pod Autoscaler that scales a rollout based on CPU metrics:

```yaml
apiVersion: autoscaling/v1
kind: HorizontalPodAutoscaler
metadata:
  name: hpa-rollout-example
spec:
  maxReplicas: 6
  minReplicas: 2
  scaleTargetRef:
    apiVersion: argoproj.io/v1alpha1
    kind: Rollout
    name: example-rollout
  targetCPUUtilizationPercentage: 80
```

## Requirements
In order for the HPA to manipulate the rollout, the Kubernetes cluster hosting the rollout CRD needs the subresources support for CRDs.  This feature was introduced as alpha in Kubernetes version 1.10 and transitioned to beta in Kubernetes version 1.11.  If a user wants to use HPA on v1.10, the Kubernetes Cluster operator will need to add a custom feature flag to the API server.  After 1.10, the flag is turned on by default.  Check out the following [link](https://kubernetes.io/docs/reference/command-line-tools-reference/feature-gates/) for more information on setting the custom feature flag.