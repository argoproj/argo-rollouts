# Traffic management

Traffic management is controlling the data plane to have intelligent routing rules for an application. These routing rules can manipulate the flow of traffic to different versions of an application enabling Progressive Delivery. These controls limit the blast radius of a new release by ensuring a small percentage of users receive a new version while it is verified.

There are various techniques to achieve traffic management:

- Raw percentages (i.e., 5% of traffic should go to the new version while the rest goes to the stable version)
- Header-based routing (i.e., send requests with a specific header to the new version)
- Mirrored traffic where all the traffic is copied and send to the new version in parallel (but the response is ignored)

## Traffic Management tools in Kubernetes

The core Kubernetes objects do not have fine-grained tools needed to fulfill all the requirements of traffic management. At most, Kubernetes offers native load balancing capabilities through the Service object by offering an endpoint that routes traffic to a grouping of pods based on that Service's selector. Functionality like traffic mirroring or routing by headers is not possible with the default core Service object, and the only way to control the percentage of traffic to different versions of an application is by manipulating replica counts of those versions. 

Service Meshes fill this missing functionality in Kubernetes. They introduce new concepts and functionality to control the data plane through the use of CRDs and other core Kubernetes resources. 

## How does Argo Rollouts enable traffic management?

Argo Rollouts enables traffic management by manipulating the Service Mesh resources to match the intent of the Rollout. Argo Rollouts currently supports the following service meshes:

- [AWS ALB Ingress Controller](alb.md)
- [Ambassador Edge Stack](ambassador.md)
- [Istio](istio.md)
- [Nginx Ingress Controller](nginx.md)
- [Service Mesh Interface (SMI)](smi.md)
- [Multiple Providers](mixed.md)
- File a ticket [here](https://github.com/argoproj/argo-rollouts/issues) if you would like another implementation (or thumbs up it if that issue already exists)

Regardless of the Service Mesh used, the Rollout object has to set a canary Service and a stable Service in its spec. Here is an example with those fields set:
```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
spec:
  ...
  strategy:
    canary:
      canaryService: canary-service
      stableService: stable-service
      trafficRouting:
       ...
```

The controller modifies these Services to route traffic to the appropriate canary and stable ReplicaSets as the Rollout progresses. These Services are used by the Service Mesh to define what group of pods should receive the canary and stable traffic.

Additionally, the Argo Rollouts controller needs to treat the Rollout object differently when using traffic management. In particular, the Stable ReplicaSet owned by the Rollout remains fully scaled up as the Rollout progresses through the Canary steps.

Since the traffic is controlled independently by the Service Mesh resources, the controller needs to make a best effort to ensure that the Stable and New ReplicaSets are not overwhelmed by the traffic sent to them. By leaving the Stable ReplicaSet scaled up, the controller is ensuring that the Stable ReplicaSet can handle 100% of the traffic at any time[^1]. The New ReplicaSet follows the same behavior as without traffic management. The new ReplicaSet's replica count is equal to the latest SetWeight step percentage multiple by the total replica count of the Rollout. This calculation ensures that the canary version does not receive more traffic than it can handle.

[^1]: The Rollout has to assume that the application can handle 100% of traffic if it is fully scaled up. It should outsource to the HPA to detect if the Rollout needs to more replicas if 100% isn't enough.
