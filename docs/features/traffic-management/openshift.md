# Openshift Routes

[Openshift Routes](https://docs.openshift.com/enterprise/3.0/architecture/core_concepts/routes.html) allow services to be exposed through externally-reachable hostnames. Openshift routes have additional functionality with traffic splitting between different services, allowing Argo Rollouts to shift traffic between different versions during a Canary deployment.

## How it works

Canary deployment is achieved by configuring the weight amounts to different backends on the Openshift route resource:

```yaml
kind: Route
apiVersion: route.openshift.io/v1
metadata:
  name: main-route
spec:
  host: >-
    https://main-route.example.com
  to:
    kind: Service
    name: stable-service
    weight: 80
  alternateBackends:
    - kind: Service
      name: canary-service
      weight: 20
  port:
    targetPort: http
  wildcardPolicy: None
```

In this example, the route is sending 80% of the traffic on the host URL to the service `stable-service` and the remaining 20% to `canary-service`. Chaning the weight fields of either the default backend (under `to`) or the canary backend (under `alternateBackends`) will result in the corresponding traffic shift. Of course, Argo Rollouts will automate the update of the weight values, given that at least one route resource exists and is specified within the rollout:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
...
spec:
  strategy:
    canary:
      stableService: stable-service
      canaryService: canary-service
      trafficRouting:
        openshift:
          routes:
            - main-route # Required
      steps:
      - setWeight: 30
      - pause: {duration: 60s}
      - setWeight: 60
      - pause: {duration: 60s}
      ...
```

Multiple routes are supported as long as each of their default backends points to the `stableService` defined in the rollout, and they do not have any alternate backends defined (since Argo Rollouts will handle the switching between services).

The Argo Rollouts controller will:
 1. Create the `alternateBackends` field in each route, pointing to the `canaryService` defined in the rollout
 2. Update the weight according to the rollout steps
 3. Delete the `alternateBackends` field once the controller finishes

For more information about each of the rollout fields, check the [specification](../specification.md)
For a start guide to using Openshift Routes with Argo Rollouts, check the [start guide](../../getting-started/openshift/index.md)