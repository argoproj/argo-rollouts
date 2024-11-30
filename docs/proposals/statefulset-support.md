---
title: Statefulset support
authors:
  - '@aburan28'
sponsors:
  - '@zaller'
creation-date: 2024-11-16
---

# Summary

Currently, Argo Rollouts does not support Stateful workloads. This gap has been a frequent topic of community discussions and GitHub issues:

[Argo Rollouts Issue #1635](https://github.com/argoproj/argo-rollouts/issues/1635)
[Argo Rollouts Issue #3502](https://github.com/argoproj/argo-rollouts/issues/3502)

Similarly, other progressive delivery controllers such as Flagger are exploring StatefulSet support:

[Flagger Issue #410](https://github.com/weaveworks/flagger/issues/410)
[Flagger PR #1391](https://github.com/fluxcd/flagger/pull/1391)

Adding StatefulSet support in Argo Rollouts will enable safe deployments for workloads requiring stable pod identities and persistent storage.




## Motivation
### Problem Statement
StatefulSet updates are inherently slow and complex due to their ordered guarantees and reliance on headless services. 
These workloads often need to preserve data integrity and quorum during updates, posing challenges for progressive delivery strategies like Canary and 
Blue/Green deployments.

### Benefits
Broader adoption of Argo Rollouts by covering critical use cases. Safe, automated updates for Stateful workloads with rollback capabilities. Better alignment with Kubernetes-native StatefulSet features like partitions and maxUnavailable.

### Goals

The goals of this proposal are:
1. Design and implement support for StatefulSet workloads in Argo Rollouts.
2. Provide progressive delivery strategies (Canary and Blue/Green) for StatefulSet workloads.
3. Maintain compatibility with existing StatefulSet guarantees without reimplementing its controller.


### Non Goals

1. Any support for Stateful workloads should not reimplement the statefulset controller nor alter guarantees that 
the statefulset controller provides. 


### StatefulSet Types
For purpose of this proposal we identify two general types of applications deployed using statefulsets

#### Type 1 
Distributed databases such as postgres, consul, etc. These typically use a headless service. Pods connect directly to other pods. These workloads are quorum sensitive. Examples would be databases such as postgres or consul. PVC's on these types of workloads might want to be snapshotted before an upgrade and automatically restored if a change fails.  

#### Type 2
Applications that use persistent storage but do not connect directly via a k8s service. Examples might include log aggregators. 


# Background 

#### Statefulset workload

One reason statefulsets are used is that they provide a stable pod identity. This can be used to associate a parituclar pod with a PVC. 


##### Rolling Updates 

There are two strategies for statefulsets

1. `OnDelete` -- This updates the statefulset pods by requiring manual user intervention in order to delete the old pods. New pods will come up with the new version. 
2. `RollingUpdate` -- this is the default. 

##### Problems

1. Statefulset updates are exceptionally slow due to the ordered guarantees. Updates occur with 1 pod at a time. 
2. Statefulset pods often need to ensure data is saved to persistent storage.
3. Pods communicate directly with other pods via headless services. This results in complications with traffic shifting. 



##### Headless service 
A big consideration with type 1 statefulsets is that traffic hits pods directly instead of hitting a k8s service when using a headless service. 
There are traffic management considerations when using headless services. ie traffic is not always captured/processed using service mesh solutions such as Istio. 

1. Istio -- headless services
https://istio.io/latest/docs/ops/configuration/traffic-management/traffic-routing/#headless-services 

##### Pod management policy

Applies only to scaling operations for statefulsets. When `managementPolicy` is set to `OrderedReady` any scaling operations will happen 1 pod at a time. The next pod deployed will have to wait until the previous pod comes up healthy. `Parallel` policy will launch the new pods all at once. 


##### Statefulset features 
1. RollingUpdate stategy supports adding a `maxUnavailable` field to ensure that rolling updates only result in 1 pod at a time. This feature is currently alpha as of 1.24 and does not seem slated for beta or stable support in the near future. 

https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/#maximum-unavailable-pods 

2. Partitioned rollouts

As of kubernetes 1.31 there is support for partitioned rolling updates https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/#partitions 
This allows developers to define behavior on statefulset updates using the ordinal index. 

By using partitions it is also possible to define ordered rollouts that can be targeted to specific pods. Ie start an update 


Example 

```yaml
spec:
  updateStrategy:
    rollingUpdate:
      partition: 10
```

If the above Statefulset has 20 replicas the pods `pod-9` through `pod-19` will be updated with the new pod spec. Pods between `pod-0` and `pod-9` will not be updated with the new version of the pod spec.

``` In most cases you will not need to use a partition, but they are useful if you want to stage an update, roll out a canary, or perform a phased roll out.```

3. `minReadySeconds` 
This can be used to inject a wait between a pod coming up and passing readiness probes and receiving live traffic.  
https://kubernetes.io/blog/2021/08/27/minreadyseconds-statefulsets/ 
https://github.com/kubernetes/enhancements/tree/master/keps/sig-apps/2599-minreadyseconds-for-statefulsets#readme

## Argo Rollouts plugins

Currently Argo Rollouts supports providing [plugins](https://argoproj.github.io/argo-rollouts/plugins/). These plugins can be referenced by canary steps in the Rollout spec. 


### Metrics

### Traffic Management






### Analysis 

#### Health of a statefulset workload 

Due to the nature of the statefulset workload analysis of the health can include things such as whether or not the database was upgraded properly. 

Items such as whether or not quorum was lost must be considered. Ultimately this should be left to developers to implement via `AnalysisTemplates` and Argo rollouts should not be opinionated about this.


## Requirements

1. Support customizability of quorum parameters. 
2. Automatic Rollbacks of Statefulsets.  
3. 




### Alternatives Considered 

#### 1. Step plugin
Implement a step plugin for statefulsets. 

#### 2. Dedicated StatefulRollout Controller
This would include a new `StatefulRollout` CRD and controller which reconciles this resource. 


## Proposal

Support [type 2 StatefulSets](#type-2) in the existing rollout controller. This is a native workload in kubernetes and with the design below, can act very similar to a rolling deploy using replicasets. 

StatefulSets can be rolled out gradually using the `updateStrategy` partitions. With existing traffic routing solutions such as Istio, a canary or blue/green update can be achieved. 

Reverting to previous revisions of a StatefulSet during an update can be achieved using ControllerRevisions. 


### Design

The rollout controller at this time is opinionated about the type of workload it is meant to handle. While the rollout CRD has a field that references a `workloadRef` which takes an arbitrary `apiVersion`, `kind`, and `name`. Ideally this can serve as an entrypoint for a variety of other workloads such as Statefulsets. 

A big part of this is that the rollout controller needs to remove several of the kubernetes deployment/replicaset-isms. Within the Rollout CRD there are several fields that reference or are opinioated about ReplicaSets/Deployments. 

For example:

```yaml
spec:
  strategy:
    canary: 
      ...
      maxUnavailable: 1
      maxSurge: "20%"
      minPodsPerReplicaSet: 2
```

### Interface

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: stateful-rollout
spec:
  workloadRef: 
    apiVersion: apps/v1
    kind: StatefulSet
    name: vector
```



## Statefulset Rollout Walkthrough 

Below are some examples of how Argo Rollouts would handle canary and blue green deploys. 

### Canary 


#### mimicked rolling update 
Let's walk through how the stateful rollout controller will perform a canary rollout for a log aggregator service (such as [vector](https://github.com/vectordotdev/vector)) using Istio. This statefulset has 10 pods. In this scenario the users want to update the container image tag to a new version ie `image: docker.io/vector:0.40.0` to `image: docker.io/vector:0.42.1`. In this scenario the user wants to scale out the replica count for the statefulset. 

Below are the yaml configurations of the Rollout.

```yaml
apiVersion: apps/v1 
kind: StatefulSet
metadata:
  name: vector
spec:
  replicas: 10
```

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: vector
spec:
  workloadRef: 
    apiVersion: apps/v1
    kind: StatefulSet
    name: vector
  minReadySeconds: 30
  autoPromotionEnabled: true
  revisionHistoryLimit: 3
  strategy:
    canary: 
      stableMetadata:
        labels:
          role: stable
      canaryMetadata:
        labels:
          role: canary
      trafficRouting:
        istio:
          virtualService:
            name: vector   
            routes:
            - primary
          destinationRule:
            name: vector
            canarySubsetName: canary
            stableSubsetName: stable
      steps:
      - setWeight: 20
      - pause: {duration: 15s}
      - setWeight: 40
      - pause: {duration: 30s}
      - setWeight: 80
      - pause: {duration: 90s}
```

This change will result in a new `ControllerRevision` and a corresponding label called `controller-revision-hash: 7e93e33`. 

Below are the default `DestinationRule` and `VirtualService` resources. 

```yaml
apiVersion: networking.istio.io/v1beta1
kind: DestinationRule
metadata:
  name: vector
  namespace: vector-system
spec:
  host: vector.vector-system.svc.cluster.local
  subsets:
    - name: stable
      labels:
        controller-revision-hash: 3eshh34
    - name: canary
      labels:
        controller-revision-hash: 7e93e33
```

```yaml
apiVersion: networking.istio.io/v1beta1
kind: VirtualService
metadata:
  name: vector
  namespace: vector-system
spec:
  hosts:
    - vector.example.com
  http:
    - name: primary
      route:
        - destination:
            host: vector.vector-system.svc.cluster.local
            subset: canary
          weight: 0
        - destination:
            host: vector.vector-system.svc.cluster.local
            subset: stable
          weight: 100
```

When the user updates to the new image the `controller-revision-hash` label will be `7e93e33`. 


Here is a breakdown of the steps. 
1. StatefulSet will be updated to 12 replicas via a partition. Within the `VirtualService` resource the weight of the canary subset would be updated to 20%. 

Below is the patch to the statefulset
```yaml
kind: StatefulSet
spec:
  replicas: 12
  managementPolicy: Parallel
  updateStrategy:
    type: RollingUpdate
    partition: 9
  template:
    metadata:
      labels:
        role: canary
    ...
    containers:
      - name: vector
        image: vector:v2
```

```yaml
apiVersion: networking.istio.io/v1beta1
kind: VirtualService
metadata:
  name: vector
  namespace: vector-system
spec:
  hosts:
    - vector.example.com
  http:
    - name: primary
      route:
        - destination:
            host: vector.vector-system.svc.cluster.local
            subset: canary
          weight: 20
        - destination:
            host: vector.vector-system.svc.cluster.local
            subset: stable
          weight: 80
```

2. After the pause step for 15 seconds the statefulset will then add more replicas with the following patch. The setting of `paritition` to 9 remains the same. 

```yaml
kind: StatefulSet
spec:
  replicas: 14
  updateStrategy:
    type: RollingUpdate
    partition: 9
  template:
    metadata:
      labels:
        role: canary
    ...
    containers:
      - name: vector
        image: vector:v2

```

```yaml
apiVersion: networking.istio.io/v1beta1
kind: VirtualService
metadata:
  name: vector
  namespace: vector-system
spec:
  hosts:
    - vector.example.com
  http:
    - name: primary
      route:
        - destination:
            host: vector.vector-system.svc.cluster.local
            subset: canary
          weight: 40
        - destination:
            host: vector.vector-system.svc.cluster.local
            subset: stable
          weight: 60
```

3. this will now update the statefulset to 20 replicas and increase the canary traffic weight to 80%. 


```yaml
kind: StatefulSet
spec:
  replicas: 20
  managementPolicy: Parallel
  updateStrategy:
    type: RollingUpdate
    partition: 9
  template:
    metadata:
      labels:
        role: canary
    containers:
      - name: vector
        image: vector:v2
```

```yaml
apiVersion: networking.istio.io/v1beta1
kind: VirtualService
metadata:
  name: vector
  namespace: vector-system
spec:
  hosts:
    - vector.example.com
  http:
    - name: primary
      route:
        - destination:
            host: vector.vector-system.svc.cluster.local
            subset: canary
          weight: 80
        - destination:
            host: vector.vector-system.svc.cluster.local
            subset: stable
          weight: 20
```

4. Now that the steps have been completed the change will be promoted. This will trigger the following patch which will reduce the pod count from 20 to 10 and will update the pods from partition 0 aka pods `vector-0` through `vector-9`. 


```yaml
apiVersion: apps/v1
kind: StatefulSet
spec:
  replicas: 10
  managementPolicy: Parallel
  updateStrategy:
    type: RollingUpdate
    partition: 0
  template:
    metadata:
      labels:
        role: stable
    containers:
      - name: vector
        image: vector:v2
```

```yaml
apiVersion: networking.istio.io/v1beta1
kind: VirtualService
metadata:
  name: vector
  namespace: vector-system
spec:
  hosts:
    - vector.example.com
  http:
    - name: primary
      route:
        - destination:
            host: vector.vector-system.svc.cluster.local
            subset: canary
          weight: 0
        - destination:
            host: vector.vector-system.svc.cluster.local
            subset: stable
          weight: 100
```


#### regular updates without scaling up

Another example show cases how a rollout will occur for a statefulset that does not require scaling out the replica count. 

```yaml
apiVersion: apps/v1 
kind: StatefulSet
metadata:
  name: vector
spec:
  replicas: 10
```

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: vector
spec:
  workloadRef: 
    apiVersion: apps/v1
    kind: StatefulSet
    name: vector
  minReadySeconds: 30
  autoPromotionEnabled: true
  revisionHistoryLimit: 3
  strategy:
    canary: 
      stableMetadata:
        labels:
          role: stable
      canaryMetadata:
        labels:
          role: canary
      trafficRouting:
        istio:
          virtualService:
            name: vector   
            routes:
            - primary
          destinationRule:
            name: vector
            canarySubsetName: canary
            stableSubsetName: stable
      steps:
      - setWeight: 50
      - pause: {duration: 30s}
      - setWeight: 80
      - pause: {duration: 90s}
```

1. First step changes 50% of the pods to use the new image tag

```yaml
apiVersion: apps/v1
kind: StatefulSet
spec:
  replicas: 10
  updateStrategy:
    type: RollingUpdate
    partition: 5
  template:
    metadata:
      labels:
        role: canary
    containers:
      - name: vector
        image: vector:v2
```

```yaml
apiVersion: networking.istio.io/v1beta1
kind: VirtualService
metadata:
  name: vector
  namespace: vector-system
spec:
  hosts:
    - vector.example.com
  http:
    - name: primary
      route:
        - destination:
            host: vector.vector-system.svc.cluster.local
            subset: canary
          weight: 50
        - destination:
            host: vector.vector-system.svc.cluster.local
            subset: stable
          weight: 50
```

2. Second step rolls out the change to 80% of the replicas and shifts the weight of the canary to 80% of traffic 

```yaml
apiVersion: apps/v1
kind: StatefulSet
spec:
  replicas: 10
  updateStrategy:
    type: RollingUpdate
    partition: 2
  template:
    metadata:
      labels:
        role: canary
    containers:
      - name: vector
        image: vector:v2
```

```yaml
apiVersion: networking.istio.io/v1beta1
kind: VirtualService
metadata:
  name: vector
  namespace: vector-system
spec:
  hosts:
    - vector.example.com
  http:
    - name: primary
      route:
        - destination:
            host: vector.vector-system.svc.cluster.local
            subset: canary
          weight: 80
        - destination:
            host: vector.vector-system.svc.cluster.local
            subset: stable
          weight: 20
```

3. Last step is automatic promotion in which the following patches are applied 

```yaml
apiVersion: apps/v1
kind: StatefulSet
spec:
  replicas: 10
  updateStrategy:
    type: RollingUpdate
    partition: 0
  template:
    metadata:
      labels:
        role: stable
```

```yaml
apiVersion: networking.istio.io/v1beta1
kind: VirtualService
metadata:
  name: vector
  namespace: vector-system
spec:
  hosts:
    - vector.example.com
  http:
    - name: primary
      route:
        - destination:
            host: vector.vector-system.svc.cluster.local
            subset: canary
          weight: 0
        - destination:
            host: vector.vector-system.svc.cluster.local
            subset: stable
          weight: 100
```

#### Rollback 

Example below is a failed update where the image results in `CrashLoopBackoff` errors. 

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: vector
spec:
  workloadRef: 
    apiVersion: apps/v1
    kind: StatefulSet
    name: vector
  minReadySeconds: 30
  autoPromotionEnabled: true
  revisionHistoryLimit: 3
  strategy:
    canary: 
      stableMetadata:
        labels:
          role: stable
      canaryMetadata:
        labels:
          role: canary
      trafficRouting:
        istio:
          virtualService:
            name: vector   
            routes:
            - primary
          destinationRule:
            name: vector
            canarySubsetName: canary
            stableSubsetName: stable
      steps:
      - setWeight: 50
      - pause: {duration: 15s}
      - setWeight: 80
      - pause: {duration: 30s}
```

1. 50% of pods updated with the new v2 tag and 50% of traffic cut-over to the new canary pods. 

```yaml
apiVersion: apps/v1
kind: StatefulSet
spec:
  replicas: 10
  updateStrategy:
    type: RollingUpdate
    partition: 5
  template:
    metadata:
      labels:
        role: canary
    containers:
      - name: vector
        image: vector:v2
```

```yaml
apiVersion: networking.istio.io/v1beta1
kind: VirtualService
metadata:
  name: vector
  namespace: vector-system
spec:
  hosts:
    - vector.example.com
  http:
    - name: primary
      route:
        - destination:
            host: vector.vector-system.svc.cluster.local
            subset: canary
          weight: 50
        - destination:
            host: vector.vector-system.svc.cluster.local
            subset: stable
          weight: 50
```

2. After this change the error rate spikes and this change will need to be rolled back. 


```yaml
apiVersion: apps/v1
kind: StatefulSet
spec:
  replicas: 10
  updateStrategy:
    type: RollingUpdate
    partition: 5
  template:
    metadata:
      labels:
        role: stable
    containers:
      - name: vector
        image: vector:v1
```

```yaml
apiVersion: networking.istio.io/v1beta1
kind: VirtualService
metadata:
  name: vector
  namespace: vector-system
spec:
  hosts:
    - vector.example.com
  http:
    - name: primary
      route:
        - destination:
            host: vector.vector-system.svc.cluster.local
            subset: canary
          weight: 0
        - destination:
            host: vector.vector-system.svc.cluster.local
            subset: stable
          weight: 100
```

### Blue/Green

`blue-service`

`green-service`


### User stories

As a platform maintainer I would like to offer ways to safely upgrade statefulsets using blue/greeen and canary upgrade strategies. 

As a developer I would like to perform safe upgrades of statefulset services with automated rollbacks. 


### Open Questions

1. How can we suspend the StatefulSet such that we don't have to worry about git changes overriding rollouts. Rollouts currently uses the `spec.paused` field on Deployments to prevent the Deployment from creating new replicasets. What would be the corresponding way to do this with StatefulSets? 
  1a. a if we cannot achieve this with a paused parameter we may need to handle the creation of the statefulsets in the controller. 



## Appendix



https://github.com/kubernetes/enhancements/blob/master/keps/sig-apps/961-maxunavailable-for-statefulset/README.md
https://openkruise.io/docs/user-manuals/advancedstatefulset/

