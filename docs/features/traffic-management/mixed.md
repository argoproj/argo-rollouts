# Multiple Providers
!!! note

    Multiple trafficRouting is available since Argo Rollouts v1.1

The usage of multiple providers tries to cover scenarios where, for some reason, we have to use
different providers on North-South and West-East traffic routing or any other hybrid architecture that
requires the use of multiple providers.

## When to use multiple providers

When you do not want:

* Inject sidecars on your Ingress controller, a common requirement of the service mesh.
* Manipulate the host header as result of introducing the ingress controller into the service mesh.
* Replace your ingress controller with VirtualGateways or similars.
* Change provider with [big-bang-adoption](https://en.wikipedia.org/wiki/Big_bang_adoption)

When you want:

* Perform North-South traffic routing with NGiNX and West-East traffic routing with SMI simultaneously
* Gradually shift between provider A to provider B on a hybrid architecture, NGiNX to Istio
* Adopt canary gradually on a hybrid architecture with both North-South and West-East traffic routing, for example NGiNX and SMI.

## Requirements

The use of multiple providers requires that both providers comply with its minimum requirements independently.
By example, if you wan't to use NGiNX and SMI you would need to have both SMI and NGiNX in place and produce the rollout configuration
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
