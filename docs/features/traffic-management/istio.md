# Istio

[Istio](https://istio.io/) is one of the most popular service mesh in the community and offers a rich feature-set to control the flow of traffic. Istio offers this functionality through a set of CRDs, and the Argo Rollouts controller modifies these resources to manipulate the traffic routing into the desired state. However, the Argo Rollouts controller modifies the Istio resources minimally to gives the developer flexibility while configuring their resources.

## Istio and Rollouts

The Argo Rollout controller achieves traffic shaping by manipulating the Virtual Service. A Virtual Service provides the flexibility to define how to route traffic when a host address is hit.  The Argo Rollouts controller manipulates the Virtual Service by using the following Rollout configuration:

- Canary Service name
- Stable Service name
- Virtual Service Name
- Which HTTP Routes in the Virtual Service

Below is an example of a Rollout with all the required fields configured:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: rollout-example
spec:
  ...
  strategy:
    canary:
      steps:
      - setWeight: 5
      - pause:
          duration: 10m
      canaryService: canary-svc # required
      stableService: stable-svc  # required
      trafficRouting:
        istio:
           virtualService: 
            name: rollout-vsvc  # required
            routes:
            - primary # At least one route is required
```


The controller looks for both the canary and stable service listed as destination hosts within the HTTP routes of the specified Virtual Service and modifies the weights of the destinations as desired. The above Rollout expects that there is a virtual service named `rollout-vsvc` with an HTTP route named `primary`. That route should have exactly two destinations:  `canary-service` and `stable-svc` Services. The `canary-svc` and `stable-svc` are required because they indicate to Istio which Pods are the Stable and Canary pods. Here is a Virtual Service that works with the Rollout specified above:

```yaml
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: rollout-vsvc
spec:
  gateways:
    - istio-rollout-gateway
  hosts:
    - istio-rollout.dev.argoproj.io
  http:
    - name: primary
      route:
        - destination:
            host: stable-svc
          weight: 100
        - destination:
            host: canary-svc
          weight: 0
```

When the above Rollout progresses through its steps, the controller changes Virtual Service's `stable-svc`'s weight to 95 and `canary-svc`'s to 5, wait 5 minutes, and then scales up the canary ReplicaSet to a full replica count. Once it is entirely healthy, the controller changes `stable-svc`'s selector to point at the canary ReplicaSet and switch the weight back to 100 to `stable-svc` and 0 to `canary-svc`.

!!! note 
    The Rollout does not make any other assumptions about the fields within the Virtual Service or the Istio mesh. The user could specify additional configurations for the virtual service like URI rewrite rules on the primary route or any other route if desired. The user can also create specific destination rules for each of the services. 


## Integrating with GitOps
The above strategy introduces a problem for users practicing GitOps. The Rollout requires the user-defined Virtual Service to define an HTTP route with both destinations hosts. However, Istio requires routes with multiple destinations to assign a weight to each destination. Since the Argo Rollout controller modifies these Virtual Service's weights as a Rollout progresses through its steps, the Virtual Service becomes out of sync with the Git version.
Additionally, if a GitOps tool does an apply after the Argo Rollouts controller changes the Virtual Service's weight, the apply would revert the weight to the percentage stored in the Git repo. At best, the user can specify the desired weight of 100% to the stable service and 0% to the canary service. In this case, the Virtual Service is synced with the Git repo when the Rollout completed all the steps. 

Argo CD has an [open issue here](https://github.com/argoproj/argo-cd/issues/2913) to address this problem. The proposed solution is to introduce an annotation to the VirtualService which tells Argo CD controller to respect the current weights listed and let the Argo Rollouts controller manage them instead.

## Alternatives Considered

### Rollout ownership over the Virtual Service  

Instead of the controller modifying a reference to a Virtual Service, the Rollout controller would create, manage, and own a Virtual Service. While this approach is GitOps friendly, it introduces other issues:

*  To provide the same flexibility as referencing Virtual Service within a Rollout, the Rollout needs to inline a large portion of the Istio spec. However, networking is outside the responsibility of the Rollout and makes the Rollout spec unnecessary complicated.
* If Istio introduces a feature, that feature will not be available in Argo Rollouts until implemented within Argo Rollouts.

Both of these issues adds more complexity to the users and Argo Rollouts developers compared to referencing a Virtual Service.

### Istio support through the [SMI Adapter for Istio](https://github.com/servicemeshinterface/smi-adapter-istio)

[SMI](https://smi-spec.io/) is the Service Mesh Interface, which serves as a standard interface for all common features of a service mesh. This feature is GitOps friendly, but native Istio has extra functionality that SMI does not currently provide.
