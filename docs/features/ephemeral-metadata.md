# Ephemeral Metadata

!!! important

    This is an **optional** feature of Argo Rollouts that allows you to have more visibility while a rollout is in progress. You do **NOT** need to use ephemeral metadata in order to achieve the main functionality of Argo Rollouts.

Normally during a deployment, Argo Rollouts automatically handles the pods of the new and old versions along with their labels and their association with your traffic provider (if you use one).

In some scenarios however,

1. You might want to annotate the pods of each version with your own custom labels
1. You may want the application itself to know when a deployment is happening 


Argo Rollouts gives you the capability to label or annotate the desired/stable pods with user-defined
labels/annotations, for _only_ the duration which they are the desired or stable set, and for the
labels to be updated/removed as soon as the ReplicaSet switches roles (e.g. from desired to stable).

In the first use case this allows prometheus, wavefront, datadog queries and dashboards
to be built, which can rely on a consistent list of labels, rather than the `rollouts-pod-template-hash`
which is unpredictable and changing from revision to revision.

In the second use case you can have your application read the labels itself using the [Kubernetes Downward API](https://kubernetes.io/docs/concepts/workloads/pods/downward-api/) and adjust
its behavior automatically only for the duration of the canary/blue/green deployment. For example you could point your application
to a different Queue server while the application pods are in "preview" and only use the production instance of your Queue server
when the pods are marked as "stable". 

## Using Ephemeral labels

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

A Rollout using the blue-green strategy has the ability to attach ephemeral metadata to the active
or preview Pods using the `activeMetadata` and `previewMetadata` fields respectively.

```yaml
spec:
  strategy:
    blueGreen:
      activeMetadata:
        labels:
          role: active
      previewMetadata:
        labels:
          role: preview
```

During an update, the Rollout will create the desired ReplicaSet while also merging the metadata
defined in `canaryMetadata`/`previewMetadata` to the desired ReplicaSet's `spec.template.metadata`.
This results in all Pods of the ReplicaSet being created with the desired metadata. 

When the rollout
becomes fully promoted, the desired ReplicaSet becomes the stable, and is updated to use the labels
and annotations under `stableMetadata`/`activeMetadata`. The Pods of the ReplicaSet will then be
updated _in place_ to use the stable metadata (without recreating the pods).

!!! tip
    In order for tooling to take advantage of this feature, they would need to recognize the change in
    labels and/or annotations that happen _after_ the Pod has already started. Not all tools may detect
    this. For application code apart from the Kubernetes Downward API you also need a programming library that automatically reloads configuration files when they change their contents.

## Avoid Ephemeral metadata overrides

Ephemeral metadata is meant to fully describe the labels and annotations of the stable/canary (or active/preview) pods.
You should **not** define the same label or annotation key both in `spec.template.metadata` and in any of
`canaryMetadata`, `stableMetadata`, `previewMetadata` or `activeMetadata`. For example, the following is discouraged:

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
  template:
    metadata:
      labels:
        role: stable # Do not do this. "role" is already defined in ephemeral metadata
```

Using ephemeral metadata to "override" a value from the pod template is ambiguous. For example it is not clear if the value from the pod
template should be restored after the ReplicaSet switches roles or not (see [#2916](https://github.com/argoproj/argo-rollouts/issues/2916)).

To migrate, define the key **only** in the ephemeral metadata fields and remove it from `spec.template.metadata`. You do not need the
pod template to provide a "default" value, as `stableMetadata`/`activeMetadata` is also applied when no update is in progress.

!!! warning
    Currently the Argo Rollouts controller prints a warning in its logs (when the Rollout is created or updated)
    and `kubectl argo rollouts lint` prints a warning if the same key is defined in both places.
    In a future version of Argo Rollouts this will become a validation error.
