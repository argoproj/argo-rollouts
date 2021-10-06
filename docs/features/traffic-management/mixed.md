# Multiple Providers
!!! note

    Multiple trafficRouting is available since Argo Rollouts v1.2

The usage of multiple providers tries to cover scenarios where, for some reason, we have to use
different providers on North-South and West-East traffic routing or any other hybrid architecture that
requires the use of multiple providers.

## Examples of when you can use multiple providers

### Avoid injecting sidecars on your Ingress controller

This is a common requirement of the service mesh and with multiple trafficRoutings you can leverage North-South traffic shifting to NGiNX 
and West-East traffic shifting to SMI, avoiding the need of adding the Ingress controller inside the mesh.

### Avoid manipulation of the host header at the Ingress

Another common side effect of adding some of the Ingress controllers into the mesh, and is caused by the usage of those 
mesh host headers to be pointing into a mesh hostname in order to be routed.

### Avoid Big-Bang

This takes place on existing fleets where downtime is very reduced or nearly impossible.
To avoid [big-bang-adoption](https://en.wikipedia.org/wiki/Big_bang_adoption) the use of multiple providers can ease
how teams can implement gradually new technologies. An example, where an existing fleet that is using a provider
such as Ambassador and is already performing canary in a North-South fashion as part of their rollouts can gradually 
implement more providers such as Istio, SMI, etc.

### Hybrid Scenarios

In this case, its very similar to avoiding the Big-Bang, either if it is part of the platform roadmap or a new redesign
of the architecture, there are multiple scenarios where having the capacity of using multiple trafficRoutings is very 
much in need: gradual implementation, eased rollback of architecture or even for a fallback.

## Requirements

The use of multiple providers requires that both providers comply with its minimum requirements independently.
By example, if you want to use NGiNX and SMI you would need to have both SMI and NGiNX in place and produce the rollout configuration
for both.

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
