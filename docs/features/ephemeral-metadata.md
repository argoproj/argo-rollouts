# Ephemeral Metadata

!!! important
    Available since v0.10.0

One use case is for a Rollout to label or annotate the canary/stable pods with user-defined labels/annotations,
for *only* the duration which they are the canary or stable set, and for the labels to be updated/removed as soon
as the ReplicaSet switches roles (e.g. from canary to stable). The use case which this enables, is to allow
prometheus, wavefront, datadog queries and dashboards to be built, which can rely on a consistent
labels, rather than the `rollouts-pod-template-hash` which is unpredictable and changing from revision
to revision.

A Rollout using the canary strategy has the ability to attach ephemeral metadata to the stable or 
canary Pods using the `stableMetadata` and `canaryMetadata` fields respectively.

```yaml
spec:
  strategy:
    canary:
      stableMetadata:
        labels:
          role: stable
      canaryMetadata:
        labels:
          role: canary
```

During an update, the Rollout will create the canary ReplicaSet while also merging the metadata
defined in `canaryMetadata` to the canary ReplicaSet's `spec.template.metadata`. This results in all
Pods of the ReplicaSet being created with the canary metadata. When the rollout becomes fully
promoted, the canary ReplicaSet becomes the stable, and is updated to use the labels and annotations
under `stableMetadata`. The Pods of the ReplicaSet will then be updated *in place* to use the stable
metadata (without recreating the pods).

!!! important
    In order for tooling to take advantage of this feature, they would need to recognize the change
    in labels and/or annotations that happen *after* the Pod has already started. Not all tools may
    detect this.
