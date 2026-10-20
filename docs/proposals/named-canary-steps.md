---
title: Named Canary Steps
authors:
  - '@kostis-codefresh'
creation-date: 2026-10-05
---

# Named Canary Steps

Canary steps in Argo Rollouts are identified only by their position in the `steps` array. This works
great for several workloads that have a simple process. But several companies have a complex canary process
and want an easy way to identify canary steps in order to show them in different internal dashboards and reports.

This document proposes adding two optional fields, `name` and `description`, to every canary step so
that users and tools can identify a step and explain why it is there. Both fields are optional
so existing Rollout do not need any changes. 

## Summary

A canary strategy is a list of steps such as `setWeight`, `pause`, `analysis`, `experiment` and `plugin`.
Today the only way to refer to a step is by its index (for example `Step: 3/8` in the CLI). For longer
strategies this is hard to read, easy to get wrong when steps are added or removed, and gives no
information about the intent of each step.

Adding an optional, human-friendly `name` and a free-text `description` to each step makes rollouts
easier to understand in the CLI and the UI, without changing how steps run.

## Motivation

- Long canary strategies (10+ steps) are hard to follow when every step is just an index.
- A step index changes when steps are inserted or removed, so runbooks, dashboards and alerts that
  mention "step 4" go stale.
- The intent of a step ("wait for the EU business day", "smoke test against the payments API") is
  currently written in YAML comments, which are lost once the manifest is applied and are not visible
  in the UI, CLI or notifications.
- Other progressive delivery and CI/CD tools commonly let users name their stages, and users expect
  the same here.

### Goals

- Add an optional `name` field to every canary step.
- Add an optional `description` field to every canary step.
- Show the name (and the description where it fits) of the current step in the CLI and the UI.
- Stay fully backwards compatible: existing Rollouts behave exactly as before.

### Non-Goals

- Changing how any step type runs.
- Referring to steps by name in other features (for example "promote to step `canary-50`" or
  "jump to step"). Names make this possible later, but it is out of scope here.
- Adding names to blue-green strategies (they have no steps).
- Naming steps of `RolloutPlugin` or other CRDs (they can adopt the same fields later).

## Proposal

Add two optional fields to the `CanaryStep` struct. They are metadata only and sit next to the
existing step type fields.

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: example-rollout
spec:
  strategy:
    canary:
      steps:
        - name: first-canary
          description: Send a small amount of traffic to the new version
          setWeight: 10
        - name: wait-for-business-hours
          description: Give the on-call team time to watch the dashboards
          pause: {duration: 1h}
        - name: smoke-test
          description: Run the payment API smoke tests against the canary
          analysis:
            templates:
              - templateName: payments-smoke-test
        - setWeight: 50       # name and description are optional
        - pause: {}
```

### Use cases

#### Readable progress in the CLI

```
Name:            example-rollout
Status:          ॥ Paused
Strategy:        Canary
  Step:          2/5 (wait-for-business-hours)
  SetWeight:     10
```

#### Readable progress in the UI

The steps widget of the Rollout page shows the step name as the step title (falling back to the
current behaviour when no name is set) and the description as a tooltip or subtitle.

#### Self-documenting manifests

Platform teams that provide shared Rollout templates can explain each step in the manifest itself.
Unlike YAML comments, the explanation is kept in the cluster and shown to application teams.

### Implementation Details/Notes/Constraints

#### API changes

```go
type CanaryStep struct {
	// Name is an optional, human-friendly identifier for this step
	// +optional
	Name string `json:"name,omitempty" protobuf:"bytes,10,opt,name=name"`
	// Description is an optional, free-text explanation of this step
	// +optional
	Description string `json:"description,omitempty" protobuf:"bytes,11,opt,name=description"`

	SetWeight *int32 `json:"setWeight,omitempty" protobuf:"varint,1,opt,name=setWeight"`
	// ... existing fields unchanged
}
```

#### Validation

- `name` is optional. When set, it must be unique within `spec.strategy.canary.steps` and must match
  a DNS-1123 label-like pattern (lowercase alphanumerics and `-`, max 63 characters). This keeps
  names safe to use in labels, annotations and future references.
- `description` is optional, free text, with a reasonable maximum length (for example 256 characters).
- A step that only sets `name`/`description` but no step type is invalid (the existing
  "exactly one step type per step" validation stays in place).

#### Step hash

The controller detects changes to the canary steps by hashing the JSON of the steps
(`ComputeStepHash`), and **a hash change resets `currentStepIndex` to 0**.

- Because both fields use `omitempty`, Rollouts that don't set them keep exactly the same hash after
  the upgrade. No rollout restarts.
- Names and descriptions are expected to be set once, while a Rollout is **not** in progress (for
  example when the strategy is first written, or together with other step changes).
- When a Rollout is fully promoted (the new ReplicaSet is the stable one), the controller already
  handles a step change by moving straight to the last step, so it does not restart anything.

The proposal is therefore to **keep `name` and `description` in the step hash** and leave
`ComputeStepHash` unchanged. Alternatively if we expect users to change
name and description all the time we can leave them out of the hash. 
I am fine either way.

#### CLI and UI

- `kubectl argo rollouts get rollout` shows the name of the current step next to the index. (Optional)
- The UI steps widget uses the name as the step title and shows the description.
- Steps without a name are shown exactly as today.

#### Step plugins

`stepPluginStatuses[].name` already holds the *plugin* name. The step name is a separate field, and
the docs should make the difference clear. Plugin authors can read the step name from the Rollout
object they already receive.


## Security Considerations

None. The fields are free-text metadata and are not used to run anything. Length and pattern
validation keeps them from being misused for very large or unsafe values.

## Risks and Mitigations

- **Editing a name or description during a rollout restarts the canary steps.**
  Mitigation: the docs state that names and descriptions should be set while no rollout is in
  progress.
- **Users might assume names can already be used in other commands (promote/abort to step).**
  Mitigation: the docs state that names are informational for now.
- **Confusion with the plugin `name` in step plugin statuses.**
  Mitigation: clear documentation and examples.

## Upgrade / Downgrade Strategy

- **Upgrade:** Fully backwards compatible. The fields are optional, and existing Rollouts keep the
  same step hash, so no rollout in progress is restarted.
- **Downgrade:** An older controller ignores the unknown fields, and an older CRD prunes them (or rejects
  them if strict validation is on). Rollouts without names are not affected. For a Rollout that uses
  names, the older controller computes a step hash without them, so a rollout in progress at
  downgrade time restarts its steps. Downgrading while no rollout is in progress avoids this.

## Drawbacks

I don't see any. It is a purely additive feature.

## Alternatives

- **YAML comments**: what users do today. They are not kept in the cluster and are not visible to tools.
- **Annotations on the Rollout** (for example a JSON map of index → name): hard to keep in sync with
  the steps and easy to break when steps are reordered.
- **Only `name`, no `description`**: simpler, but the main request is to also record *why* a step
  exists, which a short name cannot do.

