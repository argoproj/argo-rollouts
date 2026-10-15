---
title: Declarative Promotion and Retained Traffic Rollback
authors:
  - "@bjzhangshuai"
creation-date: "2026-10-15"
---

# Declarative Promotion and Retained Traffic Rollback

## Status

Draft proposal and implementation for community discussion. This is not an
accepted API or a claim of support in an existing release. The controller,
traffic-router extension, and core lifecycle changes are presented together so
that the complete behavior can be reviewed. They can be split into dependent
PRs after agreement on the API and ownership boundaries.

## Summary

Introduce a namespaced `RolloutPromotion` resource and an optional, separately
deployed controller. A delivery system can submit an authorized promotion as
desired state instead of executing a CLI in a pipeline worker. The controller
waits for readiness, validates the bound target, records a durable operation,
and resumes reconciliation after interruption.

The proposal also includes opt-in traffic snapshots and a configurable Pod
annotation traffic-routing plugin. For integrations that route directly to
retained Pods, this permits traffic-only rollback without rewriting the
Rollout PodTemplate or recreating the selected Pods.

## Motivation

Application deployment and permission to receive production traffic are often
separate operations, with tests and approvals between them. Pipelines should
not need a continuously available worker or a long-lived cluster credential
to bridge these operations.

Readiness is asynchronous. All application Pods being Ready does not imply that
the Rollout has observed the newest generation, verified traffic routing, and
persisted its promotion pause. A render-time check can reject a transient state
before any request reaches a controller. Disabling checks is not a safe remedy:
it can authorize another revision or another pause.

Long-lived connections add a second requirement. A deployment can need both
revisions alive while new connections move gradually. If validation fails,
operators may need to route new connections back to an already running revision
instead of waiting for a workload rollback to recreate capacity.

### Goals

* Represent promotion, full promotion, abort, retry, and workload rollback as CRs.
* Wait for transient readiness without weakening UID, template, hash, or step guards.
* Record progress before mutation and resume an interrupted operation.
* Support sequential targets and a together mode with preflight checks.
* Restore a retained traffic baseline and allow a subsequent revision to use it.
* Keep normal CLI usage and default controller installation available unchanged.

### Non-goals

* Atomic transactions across multiple Rollouts or across a load balancer and Kubernetes.
* Migrating existing TCP connections or guaranteeing connection preservation on every router.
* Recreating expired rollback capacity under the guise of traffic-only rollback.
* A universal snapshot adapter for every existing traffic-routing provider.
* An organization-specific release Chart, authentication workflow, or cloud API client.

## Architecture

```mermaid
sequenceDiagram
    participant Delivery as Delivery system
    participant API as Kubernetes API
    participant Promotion as Promotion controller
    participant Rollouts as Rollouts controller
    participant Router as Annotation-aware router
    Delivery->>API: Update application Rollout
    Rollouts->>API: Reconcile candidate, routing, pause status
    Delivery->>API: Create version-bound RolloutPromotion
    Promotion->>API: Read live target identity and readiness
    loop Until authorized pause or deadline
        Promotion->>API: Persist Pending and recheck
    end
    Promotion->>API: Persist traffic snapshot and Committed operation
    Promotion->>API: Apply authorized promotion action
    Rollouts->>API: Execute configured traffic steps
    Router->>API: Observe configured annotation values
    Promotion->>API: Persist verified result
    Delivery->>API: Request traffic snapshot restoration
    Promotion->>API: Validate retained capacity and pin baseline
    Promotion->>API: Restore baseline annotations; candidate weight becomes zero
```

### Request and operation identity

`spec.requestID` identifies a forward action on one CR. With no explicit ID,
generation supplies the identity. `status.operation` records Prepared,
Committed, Applying, Verifying, Stalled, and terminal phases. Per-target
checkpoints allow a restarted controller to observe whether an action already
occurred instead of blindly advancing another step. Processed IDs are bounded
history, not an infinite exactly-once ledger.

An already completed, identity-matching target is a no-op. A replacement UID,
changed desired template, unauthorized step, or mismatched baseline remains a
failure. Readiness is not permission to follow a moving target indefinitely.

Deleting a CR also deletes its status and snapshots. Recreating it is not a
continuation of the deleted operation. Use a persistent CR, fresh IDs for new
actions, and version guards. Coordinate other writers to the same Rollout.

### Retained traffic rollback

Before forward mutation, status records the serving hash, target identity by
name/namespace and immutable Rollout UID, strategy, and versioned provider-owned
configuration/state. A distinct annotation request ID asks the same CR to
restore its latest snapshot. This is intentionally different from
`spec.action: rollback`, which invokes workload-template rollback semantics.

The target ReplicaSet must still exist, be controlled by the Rollout, have
running replicas, and be fully available with Ready Pods. The controller refuses
to recreate Pods when these conditions fail. A traffic override prevents normal
routing reconciliation from immediately undoing the restored baseline. The
Rollouts controller protects the selected hash from scale-down. On the next
desired revision, it uses the restored baseline rather than the rejected
candidate; the plugin exposes the new candidate with zero production weight.

Retention time is configured on the Rollout, not inferred from snapshot history.
History does not reserve capacity. Retention limits, autoscaling, node loss,
external writers, and Pod eviction can make a snapshot unusable.

### Routing integration

Promotion orchestration does not write provider-specific resources. A registry
resolves a compiled-in adapter implementing validation, capture, apply, verify,
and restore. Unknown providers, unsupported strategies, and incompatible
snapshot versions fail closed. Additional adapters can be registered through
`WithTrafficProviders`; CRs cannot load executable code.

Two adapters are included:

* `podAnnotation` writes Pod metadata and ReplicaSet template annotations. The
  existing `weightSwitch` API is translated to this adapter at the boundary.
* `serviceSelector` restores the active Service of a blue-green Rollout without
  touching any Pod annotations. Its snapshot binds the Service UID as well as
  the Rollout UID. It deliberately rejects weighted-canary routing.

New snapshots use a provider/version envelope. Existing annotation snapshots
remain readable through a compatibility path; they cannot retroactively acquire
UID guarantees absent from the original record. Snapshot records contain a
serving baseline and its restoration policy, not a measured live-traffic trace.

Replica retention and the override baseline remain Rollouts lifecycle concerns.
The core controller honors the blue-green Service override so normal
reconciliation cannot immediately undo restoration. Canary steps continue to
use Rollouts TrafficRouters; the promotion controller is not a second writer of
each intermediate percentage.

An additional router must also honor the retained override in its normal
Rollouts reconciliation, or ordinary routing can overwrite a restore. Registering
a promotion adapter alone does not add that lifecycle support to an existing
TrafficRouter. The included Service and annotation implementations cover both
sides of this contract; other integrations require their own validation.

The optional `argoproj/pod-annotation-traffic-router` plugin accepts a configurable
annotation key, selector, weight templates, scale, role labels, and formatting.
It supports direct and aggregate-per-Pod weight calculations. It patches Pod
metadata and ReplicaSet templates without changing container specifications.

An external router must actually understand those annotations. Verification
checks Kubernetes annotation convergence, not external load-balancer health or
measured traffic. Integer weights, backend counts, connection reuse, and router
semantics affect observed percentages. Header routes, mirroring, and additional
destinations are explicitly unsupported by this plugin.

For workloadRef migrations, the implementation retains opt-in lifecycle handling
for an existing Deployment baseline. It does not ship a migration Helm hook or
automatically adopt Deployments. Those annotations are an explicit integration
contract, not a default migration behavior.

## Security

Only trusted release operators should be able to create or update promotions.
The ability is comparable to performing Rollout lifecycle operations and, when
annotation switching is enabled, patching selected Pod/ReplicaSet metadata.
Targets are restricted to the CR's namespace. Duplicate targets are rejected.
Selectors must be unique to the intended workload. Status writes should remain
restricted to the controller.

The example installation grants no Secret access to the promotion controller.
Its election Lease uses a namespaced Role. The example ClusterRoleBinding is
for cluster-wide operation; installations can replace it with namespace-scoped
RoleBindings and configure the watch namespace. Do not expose this API to
untrusted tenants merely because the controller itself has cluster-wide RBAC.

## Compatibility and Installation

The promotion controller and CRD are opt-in and are not added to the default
installation overlay. The image includes additional entry points; it still
starts the existing Rollouts controller by default. Ordinary Rollout operations
remain available without a promotion CR. Enhanced traffic rollback requires
the matching core controller and router, not just a newer CRD.

Upgrade the CRD and matching controllers before submitting fields they do not
understand. Do not prune snapshots or downgrade controllers during a committed
operation. Before uninstalling, settle active requests and explicitly resolve
traffic overrides and optional adoption finalizers.

## Testing and Review

Unit tests cover readiness transitions, stale-target rejection, completed no-ops,
interruption checkpoints, rollback before/during/after promotion, retained
capacity checks, subsequent revisions, annotation formatting, and optional
adoption cleanup. Existing Rollouts reconciliation tests are included in the
regression run. Public examples use only reserved example domains.

The opt-in `test/promotione2e` suite runs against an explicitly supplied disposable
cluster. It covers Service-based blue-green and annotation-based canary traffic
rollback, native canary promotion, controller restarts, duplicate requests, and
subsequent releases after rollback. It checks Pod UIDs and restart counts and
makes HTTP requests through the blue-green Service proxy. It does not substitute
for the full upstream E2E matrix or an external router test.

Real-provider verification, connection-draining behavior, large-fleet load, and
failure injection against external load balancers require separate integration
testing. A local unit test must not be reported as proof of end-to-end network
behavior.

## Open Questions

* Should lifecycle actions share one CRD or use action-specific resources?
* Which additional traffic providers should implement the snapshot contract,
  and what provider-specific convergence guarantees can each offer?
* Should multi-resource arbitration move from deployment policy into per-Rollout
  leases for competing CRs and CLI writers?
* How should RBAC, metrics, and audit events evolve before a stable API?
* Should snapshots additionally bind ReplicaSet UIDs, and how should history
  limits interact with garbage collection?

## Alternatives

CLI Jobs provide simple actions but still require external retry, authorization,
and deduplication. Helm hooks can bridge lifecycle events but couple rendering
and release ordering to controller readiness. AnalysisTemplates express checks
inside a rollout, not an external approval request with a durable operation
record. An independent operator is possible; this proposal keeps shared types
and lifecycle semantics close to Rollouts while deploying the controller
independently.
