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

Argo Rollouts enables traffic management by manipulating the Service Mesh resources to match the intent of the Rollout. Argo Rollouts currently supports the following traffic providers:

- [AWS ALB Ingress Controller](alb.md)
- [Ambassador Edge Stack](ambassador.md)
- [Apache APISIX](apisix.md)
- [Google Cloud](google-cloud.md)
- [Gateway API](plugins.md)
- [Istio](istio.md)
- [Kong Ingress](kong.md)
- [Nginx Ingress Controller](nginx.md)
- [Service Mesh Interface (SMI)](smi.md)
- [Traefik Proxy](traefik.md)
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

## Traffic routing with managed routes and route precedence
##### Traffic router support: (Istio)

When traffic routing is enabled, you have the ability to also let argo rollouts add and manage other routes besides just
controlling the traffic weight to the canary. Two such routing rules are header and mirror based routes. When using these
routes we also have to set a route precedence with the upstream traffic router. We do this using the `spec.strategy.canary.trafficRouting.managedRoutes`
field which is an array the order of the items in the array determine the precedence. This set of routes will also be placed
in the order specified on top of any other routes defined manually. 

!!! warning

    All routes listed in managed routes will be removed at the end of a rollout or on an abort. Do not put any manually created routes in the list.


Here is an example:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
spec:
  ...
  strategy:
    canary:
      ...
      trafficRouting:
        managedRoutes:
          - name: priority-route-1
          - name: priority-route-2
          - name: priority-route-3
```


## Traffic routing based on a header values for Canary
##### Traffic router support: (Istio)

Argo Rollouts has ability to send all traffic to the canary-service based on a http request header value.
The step for the header based traffic routing is `setHeaderRoute` and has a list of matchers for the header. 

`name` - name of the header route.

`match` - header matching rules is an array of `headerName, headerValue` pairs.

`headerName` - name of the header to match.

`headerValue`-  contains exactly one of `exact` - specify the exact header value, 
`regex` - value in a regex format, `prefix` - the prefix of the value could be provided. Not all traffic routers will support
all match types.

To disable header based traffic routing just need to specify empty `setHeaderRoute` with only the name of the route.

Example:

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
        managedRoutes:
          - name: set-header-1
        istio:
          virtualService:
            name: rollouts-demo-vsvc
      steps:
      - setWeight: 20
      - setHeaderRoute: # enable header based traffic routing where
          name: "set-header-1"
          match:
          - headerName: Custom-Header1 # Custom-Header1=Mozilla
            headerValue:
              exact: Mozilla
          - headerName: Custom-Header2 # or Custom-Header2 has a prefix Mozilla
            headerValue:
              prefix: Mozilla
          - headerName: Custom-Header3 # or Custom-Header3 value match regex: Mozilla(.*)
            headerValue:
              regex: Mozilla(.*)
      - pause: {}
      - setHeaderRoute:
          name: "set-header-1" # disable header based traffic routing
```

## Traffic routing mirroring traffic to canary
##### Traffic router support: (Istio)

Argo Rollouts has ability to mirror traffic to the canary-service based on a various matching rules.
The step for the mirror based traffic routing is `setMirrorRoute` and has a list of matchers for the header.

`name` - name of the mirror route.

`percentage` - what percentage of the matched traffic to mirror

`match` - The matching rules for the header route, if this is missing it acts as a removal of the route.
All conditions inside a single match block have AND semantics, while the list of match blocks have OR semantics.
Each type within a match (method, path, headers) must have one and only one match type (exact, regex, prefix)
Not all match types (exact, regex, prefix) will be supported by all traffic routers.

To disable mirror based traffic route you just need to specify a `setMirrorRoute` with only the name of the route.

This example will mirror 35% of HTTP traffic that matches a `GET` requests and with the url prefix of `/`
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
        managedRoutes:
          - name: mirror-route
        istio:
          virtualService:
            name: rollouts-demo-vsvc
      steps:
        - setCanaryScale:
            weight: 25
      - setMirrorRoute:
          name: mirror-route
          percentage: 35
          match:
            - method:
                exact: GET
              path:
                prefix: /
      - pause:
          duration: 10m
      - setMirrorRoute:
          name: "mirror-route" # removes mirror based traffic route
```