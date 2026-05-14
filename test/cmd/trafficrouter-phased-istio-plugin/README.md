# Phased Istio Traffic Router Plugin

A custom Argo Rollouts traffic router plugin that advances multiple Istio VirtualService routes **independently and in sequence**, rather than updating all routes to the same weight simultaneously.

## Why this plugin exists

The built-in Istio traffic router applies a single `desiredWeight` to every route listed in `trafficRouting.istio.virtualService.routes` at the same time. There is no way to express "ramp route A to 100% before touching route B."

This plugin solves that by introducing **phases**: an ordered list of VirtualService HTTP routes that are ramped sequentially.

## Modes

### Proportional mode (recommended)

Each phase carries a `weight` representing its fraction of total traffic (0–100, phases must sum to 100). The Rollout's `desiredWeight` then maps directly to the fraction of total traffic served by canary pods, keeping ReplicaSet counts proportional to actual traffic at every step.

On every `SetWeight(N)` call the plugin computes each phase's target route weight from `N` and its position in the cumulative weight range:

```
phase 1 range:  [0,  w1]       → route weight = N / w1 * 100  (clamped to [0, 100])
phase 2 range:  [w1, w1+w2]    → route weight = (N - w1) / w2 * 100
...
```

All routes are updated in a single VirtualService write.

### Legacy mode

When no phase specifies a `weight`, the plugin uses the original sequential algorithm:

1. Reads the current VirtualService.
2. Walks phases in order; the first whose route has canary weight **below 100** is active.
3. Applies `desiredWeight` directly to that route's canary weight.
4. All other routes are left unchanged.

Legacy mode is preserved for backward compatibility. Existing configurations without `weight` fields continue to work unchanged.

---

When `desiredWeight` is 0 (either mode), all managed routes are reset to `stable = 100, canary = 0`.

`pause`, `analysis`, and other non-`setWeight` steps do not call the plugin — the VirtualService is untouched until the rollout continues.

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
    Route  string // .spec.http[].name in the VirtualService
    Weight int32  // fraction of total traffic (0-100); phases must sum to 100 (proportional mode)
                  // omit on all phases to use legacy mode
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
              # latest-route (catch-all) handles ~70% of traffic
              - route: latest-route
                weight: 70
              # stable-route (enterprise header-matched) handles ~30% of traffic
              - route: stable-route
                weight: 30
      steps:
      # desiredWeight = % of total traffic going to canary (= canary RS size / spec.Replicas).
      # Phase 1 (latest-route) spans desiredWeight 0–70.
      - setWeight: 14    # latest-route canary = 20%,  stable-route canary = 0%
      - pause: {duration: 10m}
      - setWeight: 35    # latest-route canary = 50%,  stable-route canary = 0%
      - pause: {duration: 10m}
      - setWeight: 70    # latest-route canary = 100%, stable-route canary = 0%  (phase 1 complete)
      - pause: {duration: 10m}
      # Phase 2 (stable-route) spans desiredWeight 70–100.
      - setWeight: 80    # latest-route canary = 100%, stable-route canary = 33%
      - pause: {duration: 10m}
      - setWeight: 90    # latest-route canary = 100%, stable-route canary = 67%
      - pause: {duration: 10m}
      - setWeight: 100   # latest-route canary = 100%, stable-route canary = 100%
```

## Weight progression walkthrough

Phases: `latest-route` weight=70, `stable-route` weight=30. `spec.replicas: 10`.

| Step | `desiredWeight` | canary RS pods | `latest-route` canary | `stable-route` canary |
|------|-----------------|----------------|-----------------------|-----------------------|
| 1    | 14              | ~1             | **20%**               | 0%                    |
| 2    | 35              | 3–4            | **50%**               | 0%                    |
| 3    | 70              | 7              | **100%**              | 0%                    |
| 4    | 80              | 8              | 100%                  | **33%**               |
| 5    | 90              | 9              | 100%                  | **67%**               |
| 6    | 100             | 10             | 100%                  | **100%**              |

At every step, `canary RS pods ≈ desiredWeight / 100 × spec.replicas` — proportional to actual traffic.

`pause` steps between `setWeight` steps do not call the plugin — the VirtualService remains unchanged until the rollout continues.

## Rollback and abort

When a rollout is aborted, the controller calls `RemoveManagedRoutes`, which resets all configured routes to `stable = 100, canary = 0` regardless of which phase was active.

A manual rollback (promoting to a previous revision) triggers `SetWeight(0)` followed by a new rollout, which resets all routes and begins phase 1 fresh.

## Limitations

- Routes are identified by name (`.spec.http[].name`). Routes without a `name` field are ignored.
- Subset names (`canarySubsetName`, `stableSubsetName`) must match the DestinationRule subsets and the VirtualService destination `.subset` fields exactly.
- The plugin does not support `SetHeaderRoute` or `SetMirrorRoute` — those return no-ops.
- Only HTTP routes are managed. TLS and TCP routes in the VirtualService are untouched.

