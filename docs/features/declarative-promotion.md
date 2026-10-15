# Declarative Promotion (Proposal)

This experimental API is proposed in [Declarative Promotion and Retained Traffic
Rollback](../proposals/declarative-promotion.md). It is not available in an
existing upstream release. Build both controllers from this branch before
trying the examples in a disposable cluster.

## Installation

Build a local image containing the existing controller, the promotion
controller, and the optional annotation router:

```sh
make controller promotion-controller pod-annotation-trafficrouter
make image IMAGE_TAG=declarative-promotion
```

Install the ordinary Rollouts manifests using that image. Separately install
the optional CRD and promotion controller, overriding the example image in an
overlay for your registry:

```sh
kubectl apply --server-side -f manifests/crds/rollout-promotion-crd.yaml
kubectl apply -k manifests/promotion-controller
```

The default overlay does not install the new controller. The optional overlay
uses two replicas with Lease-based leader election. The health endpoint is a
process liveness check, not a guarantee that an operation has completed. Inspect
CR status and the election Lease for operational state.

## Promote a Version

Deploy the application first and obtain the intended Rollout UID and current
Pod hash. Copy them into the example before applying it; do not remove the
guards to make a stale request pass. For a canary, also set the zero-based step
index that the release is authorized to advance.

```yaml
apiVersion: argoproj.io/v1alpha1
kind: RolloutPromotion
metadata:
  name: example-promotion
  namespace: default
spec:
  requestID: release-2-approval-1
  action: promote
  strategy: canary
  mode: together
  wait:
    enabled: true
    attempts: 180
    seconds: 5
  rollouts:
  - name: example
    expectedRolloutUID: REPLACE_WITH_ROLLOUT_UID
    expectedCurrentPodHash: REPLACE_WITH_CANDIDATE_HASH
    expectedCurrentStepIndex: 2
    readiness:
      requireCurrentPodHash: true
      requirePaused: true
      allowedCurrentStepIndexes: [2]
```

If submitting before `currentPodHash` exists, bind `expectedRolloutUID` and
`expectedPodTemplateHash` instead. The latter is a SHA256 digest of the compact
JSON encoding of the observed desired `spec.template`, not an image digest or
the ReplicaSet's short Pod hash. WorkloadRef users must use the resolved template
observed by Rollouts and wait for its workload generation to be observed. A
delivery integration must compute the same canonical input as the controller.

Transient readiness is Pending until its persisted deadline. An identity
mismatch fails. Already-promoted matching targets succeed without another
action. Reapplying a processed request ID does not advance another pause while
that ID remains in the same CR's history. Use a fresh ID for a new authorized
action. Do not delete the CR to retry.

`together` checks targets before starting them, then waits for results; it is
not an atomic cutover. `sequential` waits for each target before the next. All
targets must be in the request namespace.

For the [blue-green example](../../examples/declarative-promotion/bluegreen.yaml),
use `strategy: blueGreen`, active/preview Service names, and
omit canary step fields. `promoteFull` intentionally skips normal gates; do not
use it as a workaround for a missing promotion pause. `abort` and `retry` follow
Rollouts lifecycle semantics. `action: rollback` with `toRevision` or
`rollbackTarget: stable` restores the workload template and may create Pods.

## Retain Capacity and Shift Traffic

The [retained canary example](../../examples/declarative-promotion/retained-canary.yaml)
keeps candidate capacity independent of weight, holds an explicit manual gate,
then applies 20%, 50%, and 100% steps. The old ReplicaSet has an eight-hour
scale-down delay and a revision limit. These values are examples, not defaults
imposed by the promotion API.

Register the built-in plugin binary in the Rollouts controller ConfigMap:

```yaml
data:
  trafficRouterPlugins: |-
    - name: argoproj/pod-annotation-traffic-router
      location: file:///bin/rollouts-pod-annotation-trafficrouter
```

This plugin changes annotations only. `example.com/traffic-weight` is a reserved
example key, not a working ingress integration. Configure a router that consumes
your chosen schema. Its backend selection must include both retained groups;
a Service selecting only the new hash cannot route to old Pods just because
their weight annotation changes. New Pods must start with a safe zero-production
payload in the Pod template. Preview labels and annotations must not be managed
by multiple competing writers.

Add the following to the promotion request and its target:

```yaml
spec:
  canary:
    completionPolicy: completeAfterFullWeight
  trafficRollback:
    enabled: true
    mode: previousSnapshot
    historyLimit: 10
  rollouts:
  - name: example
    # Keep the identity/readiness fields from the promotion example above.
    traffic:
      provider: podAnnotation
      applyOnPromotion: false
      config:
        annotationKey: example.com/traffic-weight
        baseSelector: app=example
        activeWeight: '[{"host":"preview.example.com","weight":100},{"weight":100}]'
        inactiveWeight: '[{"host":"preview.example.com","weight":0},{"weight":0}]'
        reapplyDelaySeconds: "1"
```

`completeAfterFullWeight` allows timed steps to progress and completes the final
100%-weight pause. It does not skip intermediate indefinite manual pauses.
Completing advances the stable revision; replica retention remains the Rollout's
responsibility. `nextPause` instead completes the request upon reaching the next
pause, including a timed pause.

## Traffic-Only Rollback

For native blue-green Service routing, use the following instead of the
annotation adapter. No Pod annotations or annotation-aware ingress are required:

```yaml
spec:
  trafficRollback:
    enabled: true
  rollouts:
  - name: example
    # Keep the target identity and readiness guards.
    traffic:
      provider: serviceSelector
      applyOnPromotion: false
```

The adapter derives the active Service from the Rollout, snapshots its UID,
and restores its hash selector. Weighted canary is unsupported by this adapter.
It validates Kubernetes Service state; it does not claim external load-balancer
convergence. Old `weightSwitch` configurations remain supported as an alias for
`podAnnotation`. Do not set both `traffic` and `weightSwitch` on one target.

Providers are compiled-in extensions, not arbitrary scripts supplied in a CR.
Their versioned snapshot payloads are persisted before a forward action.
Unknown providers, versions, or mismatched object identities are rejected before
restoration; they never fall back to workload-template rollback.

Keep the same CR with its status history and submit a new rollback annotation:

```sh
kubectl annotate rolloutpromotion example-promotion \
  rollouts.argoproj.io/traffic-rollback-request=rollback-approval-1 --overwrite
kubectl get rolloutpromotion example-promotion -o yaml
```

The annotation requests restoration of the latest pre-promotion traffic
snapshot. It may interrupt the forward operation, or run after it completed,
provided the prior capacity is still retained and Ready. This is not
`spec.action: rollback`; it does not rewrite the workload template. Missing,
scaled-down, unowned, or unavailable capacity is rejected rather than recreated.
Deleting/reinstalling the promotion CR loses this history. A Helm rollback by
itself is not a traffic rollback unless an integration submits this annotation.

After rollback, a later application revision uses the restored serving baseline
and starts with zero production weight. A new version-bound forward request
clears that override. Use one active promotion resource per target and
coordinate manual CLI operations; multiple independent requests are not a
distributed transaction.

## Limits

Pod retention helps preserve existing connections only if the router drains
connections and does not terminate them on a weight change. This API cannot
guarantee unbroken sessions across node failures, application restarts, or
load-balancer behavior. Weight zero affects new traffic according to the router;
it does not move existing connections. Snapshot history is not a Pod backup.

The annotation plugin's verification confirms annotation convergence, not an
external router's health or exact request percentage. Real traffic and
long-connection behavior must be tested with the chosen integration.
