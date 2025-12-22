# Traffic Management

Traffic management refers to controlling the data plane through intelligent routing rules for an application. These routing rules manipulate traffic flow between different versions of an application, enabling Progressive Delivery. By ensuring that only a small percentage of users receive a new version while it's being verified, these controls help limit the potential impact (blast radius) of a new release.

There are various techniques to achieve traffic management:

- Raw percentages (i.e., 5% of traffic should go to the new version while the rest goes to the stable version)
- Header-based routing (i.e., send requests with a specific header to the new version)
- Mirrored traffic where all the traffic is copied and sent to the new version in parallel (but the response is ignored)

## Traffic Management Tools in Kubernetes

Core Kubernetes objects lack the fine-grained tools necessary for comprehensive traffic management. Kubernetes primarily offers native load balancing through the Service object, which provides an endpoint that routes traffic to pods based on the Service's selector. However, advanced features such as traffic mirroring or header-based routing are not possible with the default Service object. The only way to control traffic distribution between different versions of an application is by adjusting their replica counts.

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
  strategy:
    canary:
      canaryService: canary-service
      stableService: stable-service
      trafficRouting: {}
```

The controller modifies these Services to route traffic to the appropriate canary and stable ReplicaSets as the Rollout progresses. These Services are used by the Service Mesh to define what group of pods should receive the canary and stable traffic.

Additionally, the Argo Rollouts controller needs to treat the Rollout object differently when using traffic management. In particular, the Stable ReplicaSet owned by the Rollout remains fully scaled up as the Rollout progresses through the Canary steps.

Since the traffic is controlled independently by the Service Mesh resources, the controller needs to make a best effort to ensure that the Stable and New ReplicaSets are not overwhelmed by the traffic sent to them. By leaving the Stable ReplicaSet scaled up, the controller is ensuring that the Stable ReplicaSet can handle 100% of the traffic at any time[^1]. The New ReplicaSet follows the same behavior as without traffic management. The new ReplicaSet's replica count is equal to the latest SetWeight step percentage multiplied by the total replica count of the Rollout. This calculation ensures that the canary version does not receive more traffic than it can handle.

[^1]: The Rollout has to assume that the application can handle 100% of traffic if it is fully scaled up. It should outsource to the HPA to detect if the Rollout needs to more replicas if 100% isn't enough.

## Traffic Routing with Managed Routes and Route Precedence

**Traffic Router Support: Istio**

When traffic routing is enabled, Argo Rollouts can add and manage additional routes beyond just controlling the traffic weight
to the canary. These include header-based and mirror-based routes. When using these routes, you must set route precedence
with the upstream traffic router using the `spec.strategy.canary.trafficRouting.managedRoutes` field. This field accepts an
array where the order of items determines their precedence. These managed routes will be placed in the specified order above
any manually defined routes.

!!! warning

    All routes listed in managed routes will be removed at the end of a rollout or on an abort. Do not put any manually created routes in the list.

Here is an example:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
spec:
  strategy:
    canary:
      trafficRouting:
        managedRoutes:
          - name: priority-route-1
          - name: priority-route-2
          - name: priority-route-3
```

## Traffic Routing Based on Header Values for Canary

**Traffic Router Support: Istio**

Argo Rollouts can route all traffic to the canary service based on HTTP request header values.
Header-based traffic routing is configured using the `setHeaderRoute` step, which contains a list of header matchers.

`name` - The name of the header route.

`match` - An array of `headerName, headerValue` pairs defining the header matching rules.

`headerName` - The name of the header to match.

`headerValue` - Must contain exactly one of the following:

- `exact`: Specifies an exact header value to match
- `regex`: Specifies a regular expression pattern to match
- `prefix`: Specifies a prefix value to match

**Note:** Not all traffic routers support all match types.

To disable header based traffic routing just need to specify empty `setHeaderRoute` with only the name of the route.

Example:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
spec:
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
            name: 'set-header-1'
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
            name: 'set-header-1' # disable header based traffic routing
```

## Traffic Mirroring to Canary

**Traffic Router Support: Istio**

Argo Rollouts can mirror traffic to the canary service based on various matching rules.
Traffic mirroring is configured using the `setMirrorRoute` step, which includes header matchers.

`name` - The name of the mirror route.

`percentage` - The percentage of matched traffic to mirror.

`match` - Defines the matching rules for the header route. If omitted, the route will be removed.

- Multiple conditions within a single match block use AND logic
- Multiple match blocks use OR logic
- Each match type (method, path, headers) must specify exactly one match style (exact, regex, or prefix)

**Note: Not all traffic routers support all match types (exact, regex, prefix).**

To disable mirror based traffic route you just need to specify a `setMirrorRoute` with only the name of the route.

This example will mirror 35% of HTTP traffic that matches a `GET` requests and with the url prefix of `/`

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
spec:
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
            name: 'mirror-route' # removes mirror based traffic route
```
