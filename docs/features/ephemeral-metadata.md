# Ephemeral Metadata

!!! important

    This is an **optional** feature of Argo Rollouts that allows you to have more visibility while a rollout is in progress. You do **NOT** need to use emphemeral metadata in order to achieve the main functionality of Argo Rollouts

Normally during a deployment, Argo Rollouts automatically handles the pods of the new and old versions along with their labels and their association with your traffic provider (if you use one).

In some scenarios however,

1. You might want to annotate the pods of each version with your own custom labels
1. You may want the application itself know when a deployment is happening 


Argo Rollouts gives you the capability to label or annotate the desired/stable pods with user-defined
labels/annotations, for _only_ the duration which they are the desired or stable set, and for the
labels to be updated/removed as soon as the ReplicaSet switches roles (e.g. from desired to stable).

In the first use case this allows prometheus, wavefront, datadog queries and dashboards
to be built, which can rely on a consistent list of labels, rather than the `rollouts-pod-template-hash`
which is unpredictable and changing from revision to revision.

In the second use case you can have your application read the labels itself using the [Kubernetes Downward API](https://kubernetes.io/docs/concepts/workloads/pods/downward-api/) and adjust
its behavior automatically only for the duration of the canary/blue/green deployment. For example you could point your application
to a different Queue server while the application pod are in "preview" and only use the production instance of your Queue server
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
    this. For application code apart from the Kubernetes Downward API you also need a programming library that automatically reloads configuration files when they change.
