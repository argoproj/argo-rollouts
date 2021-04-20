# Ambassador Edge Stack

[Ambassador Edge Stack](https://www.getambassador.io/products/edge-stack/) provides the functionality you need at the edge your Kubernetes cluster (hence, an "edge stack"). This includes an API gateway, ingress controller, load balancer, developer portal, canary traffic routing and more. It provides a group of CRDs that users can configure to enable different functionalities. 

Argo-Rollouts provides an integration that leverages Ambassador's [canary routing capability](https://www.getambassador.io/docs/latest/topics/using/canary/). This allows the traffic to your application to be gradually incremented while new versions are being deployed.

## How it works

Ambassador Edge Stack provides a resource called `Mapping` that is used to configure how to route traffic to services. Ambassador canary deployment is achieved by creating 2 mappings with the same URL prefix pointing to different services. Consider the following example:

```yaml
apiVersion: getambassador.io/v2
kind:  Mapping
metadata:
  name: stable-mapping
spec:
  prefix: /someapp
  rewrite: /
  service: someapp-stable:80
---
apiVersion: getambassador.io/v2
kind:  Mapping
metadata:
  name: canary-mapping
spec:
  prefix: /someapp
  rewrite: /
  service: someapp-canary:80
  weight: 30
```

In the example above we are configuring Ambassador to route 30% of the traffic coming from `<public ingress>/someapp` to the service `someapp-canary` and the rest of the traffic will go to the service `someapp-stable`. If users want to gradually increase the traffic to the canary service, they have to update the `canary-mapping` setting the weight to the desired value either manually or automating it somehow. 

With Argo-Rollouts there is no need to create the `canary-mapping`. The process of creating it and gradually updating its weight is fully automated by the Argo-Rollouts controller. The following example shows how to configure the `Rollout` resource to use Ambassador as a traffic router for canary deployments:


```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
...
spec:
  strategy:
    canary:
      stableService: someapp-stable
      canaryService: someapp-canary
      trafficRouting:
        ambassador:
          mappings:
            - stable-mapping
      steps:
      - setWeight: 30
      - pause: {duration: 60s}
      - setWeight: 60
      - pause: {duration: 60s}
```

Under `spec.strategy.canary.trafficRouting.ambassador` there are 2 possible attributes:

- `mappings`: Required. At least one Ambassador mapping must be provided for Argo-Rollouts to be able to manage the canary deployment. Multiple mappings are also supported in case there are multiple routes to the service (e.g., your service has multiple ports, or can be accessed via different URLs). If no mapping is provided Argo-Rollouts will send an error event and the rollout will be aborted. 

When Ambassador is configured in the `trafficRouting` attribute of the manifest, the Rollout controller will:
1. Create one canary mapping for each stable mapping provided in the Rollout manifest
1. Proceed with the steps according to the configuration updating the canary mapping weight
1. At the end of the process Argo-Rollout will delete all the canary mappings created

## Endpoint Resolver

By default, Ambassador uses kube-proxy to route traffic to Pods. However we should configure it to bypass kube-proxy and route traffic directly to pods. This will provide true L7 load balancing which is desirable in a canary workflow. This approach is called [endpoint routing](https://www.getambassador.io/docs/latest/topics/running/load-balancer/) and can be achieve by configuring [endpoint resolvers](https://www.getambassador.io/docs/latest/topics/running/resolvers/#the-kubernetes-endpoint-resolver).

To configure Ambassador to use endpoint resolver it is necessary to apply the following resource in the cluster:

```yaml
apiVersion: getambassador.io/v2
kind: KubernetesEndpointResolver
metadata:
  name: endpoint
```

And then configure the mapping to use it setting the `resolver` attribute:

```
apiVersion: getambassador.io/v2
kind:  Mapping
metadata:
  name: stable-mapping
spec:
  resolver: endpoint
  prefix: /someapp
  rewrite: /
  service: someapp-stable:80
```

For more details about the Ambassador and Argo-Rollouts integration, see the [Ambassador Argo documentation](https://deploy-preview-508--datawire-ambassador.netlify.app/docs/pre-release/argo/).
