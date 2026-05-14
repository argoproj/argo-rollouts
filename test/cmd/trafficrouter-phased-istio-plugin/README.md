# Phased Istio Traffic Router Plugin

A custom Argo Rollouts traffic router plugin that advances multiple Istio VirtualService routes **independently and in sequence**, rather than updating all routes to the same weight simultaneously.

## Why this plugin exists

The built-in Istio traffic router applies a single `desiredWeight` to every route listed in `trafficRouting.istio.virtualService.routes` at the same time. There is no way to express "ramp route A to 100% before touching route B."

This plugin solves that by introducing **phases**: an ordered list of VirtualService HTTP routes. Each `setWeight` step advances only the first phase whose canary weight is still below 100. Once a phase reaches 100%, the next phase becomes active automatically.

## How phase detection works

On every `SetWeight(N)` call the plugin:

1. Reads the current VirtualService from the cluster.
2. Walks the configured phases in order.
3. The first phase whose named route has a canary weight **below 100** is the active phase.
4. Applies `desiredWeight` to that route (`canary = N`, `stable = 100 - N`).
5. All other routes are left unchanged.

When `desiredWeight` is 0, all managed routes are reset to `stable = 100, canary = 0` (used during rollback / `RemoveManagedRoutes`).

Because the active phase is determined by reading live cluster state, the plugin is naturally resilient to `pause`, `analysis`, and any other non-`setWeight` step types — they don't call `SetWeight`, so the VirtualService is untouched until the rollout continues.

## Installation

Build the binary and register it in the argo-rollouts ConfigMap.

### Build

```bash
go build -o trafficrouter-phased-istio-plugin \
  ./test/cmd/trafficrouter-phased-istio-plugin/
```

Place the binary somewhere the rollout controller can reach it (e.g. a shared volume, an object-store URL, or a `file://` path on the controller node).

### Register in the ConfigMap

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: argo-rollouts-config
  namespace: argo-rollouts
data:
  trafficRouterPlugins: |
    - name: argoproj/phased-istio-router
      location: "file:///usr/local/bin/trafficrouter-phased-istio-plugin"
```

The `location` field accepts an HTTPS URL or a `file://` path. If an HTTPS URL is used, an optional `sha256` field can be added for checksum verification.

## Configuration reference

The plugin is configured via a JSON (or YAML-as-JSON) string under `trafficRouting.plugins` in the Rollout spec.

```go
type PluginConfig struct {
    VirtualService  VSRef   // required
    DestinationRule DRRef   // required for subset-based routing
    Phases          []Phase // required; processed in order
}

type VSRef struct {
    Name      string // VirtualService name
    Namespace string // defaults to the Rollout's namespace
}

type DRRef struct {
    Name             string // DestinationRule name
    Namespace        string // defaults to the Rollout's namespace
    CanarySubsetName string // subset used for canary traffic
    StableSubsetName string // subset used for stable traffic
}

type Phase struct {
    Route string // .spec.http[].name in the VirtualService
}
```

The plugin also manages the DestinationRule's pod-template-hash label selectors on each `UpdateHash` call, so a `trafficRouting.istio` block is **not** needed and should not be included (both reconcilers would run and conflict on VS updates).

## Full example

### VirtualService

```yaml
apiVersion: networking.istio.io/v1
kind: VirtualService
metadata:
  name: my-service
  namespace: my-namespace
spec:
  hosts:
  - my-service.my-namespace.svc.cluster.local
  http:
  - name: stable-route
    match:
    - headers:
        User-Org-Id:
          regex: "enterprise-1|enterprise-2"
    route:
    - destination:
        host: my-service.my-namespace.svc.cluster.local
        subset: stable
      weight: 100
    - destination:
        host: my-service.my-namespace.svc.cluster.local
        subset: canary
      weight: 0
  - name: latest-route
    route:
    - destination:
        host: my-service.my-namespace.svc.cluster.local
        subset: stable
      weight: 100
    - destination:
        host: my-service.my-namespace.svc.cluster.local
        subset: canary
      weight: 0
```

### DestinationRule

```yaml
apiVersion: networking.istio.io/v1
kind: DestinationRule
metadata:
  name: my-service-destination
  namespace: my-namespace
spec:
  host: my-service.my-namespace.svc.cluster.local
  subsets:
  - name: stable
    labels:
      rollouts-pod-template-hash: ""   # managed by the plugin's UpdateHash
  - name: canary
    labels:
      rollouts-pod-template-hash: ""
```

### Rollout

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: my-service
  namespace: my-namespace
spec:
  replicas: 5
  selector:
    matchLabels:
      app: my-service
  template:
    metadata:
      labels:
        app: my-service
    spec:
      containers:
      - name: my-service
        image: my-service:latest
  strategy:
    canary:
      trafficRouting:
        plugins:
          argoproj/phased-istio-router: |
            virtualService:
              name: my-service
              namespace: my-namespace
            destinationRule:
              name: my-service-destination
              namespace: my-namespace
              canarySubsetName: canary
              stableSubsetName: stable
            phases:
              - route: latest-route
              - route: stable-route
      steps:
      # Phase 1 — ramp catch-all traffic (latest-route)
      - setWeight: 10
      - pause: {duration: 10m}
      - setWeight: 50
      - pause: {duration: 10m}
      - setWeight: 80
      - pause: {duration: 10m}
      - setWeight: 100
      # Phase 2 — ramp enterprise header-matched traffic (stable-route)
      - setWeight: 5
      - pause: {duration: 10m}
      - setWeight: 25
      - pause: {duration: 10m}
      - setWeight: 75
      - pause: {duration: 10m}
      - setWeight: 100
```

## Weight progression walkthrough

| Step | `desiredWeight` | Active phase | `latest-route` canary | `stable-route` canary |
|------|-----------------|--------------|-----------------------|-----------------------|
| 1    | 10              | latest-route | **10**                | 0                     |
| 2    | 50              | latest-route | **50**                | 0                     |
| 3    | 80              | latest-route | **80**                | 0                     |
| 4    | 100             | latest-route | **100**               | 0                     |
| 5    | 5               | stable-route | 100                   | **5**                 |
| 6    | 25              | stable-route | 100                   | **25**                |
| 7    | 75              | stable-route | 100                   | **75**                |
| 8    | 100             | stable-route | 100                   | **100**               |

`pause` steps between `setWeight` steps do not call the plugin — the VirtualService remains unchanged until the rollout continues.

## Rollback and abort

When a rollout is aborted, the controller calls `RemoveManagedRoutes`, which resets all configured routes to `stable = 100, canary = 0` regardless of which phase was active.

A manual rollback (promoting to a previous revision) triggers `SetWeight(0)` followed by a new rollout, which resets all routes and begins phase 1 fresh.

## Limitations

- Routes are identified by name (`.spec.http[].name`). Routes without a `name` field are ignored.
- Subset names (`canarySubsetName`, `stableSubsetName`) must match the DestinationRule subsets and the VirtualService destination `.subset` fields exactly.
- The plugin does not support `SetHeaderRoute` or `SetMirrorRoute` — those return no-ops.
- Only HTTP routes are managed. TLS and TCP routes in the VirtualService are untouched.

