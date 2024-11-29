---
title: Statefulset support
authors:
  - '@aburan28'
sponsors:
  - '@zaller'
creation-date: 2024-11-16
---

# Summary
Stateful workloads are currently un-supported in Argo Rollouts. This is a feature that several folks have enquired about over the years and is the subject of a few github issues in the argo rollouts repo. 
https://github.com/argoproj/argo-rollouts/issues/1635
https://github.com/argoproj/argo-rollouts/issues/3502

Additionally, several other progressive delivery controllers such as flagger also have open issues discussing statefulset support. 

https://github.com/weaveworks/flagger/issues/410
https://github.com/fluxcd/flagger/pull/1391 


## Motivation
Adding support will increase adoption and cover an important use-case. By providing rollout support for statefulsets developers can safely deploy updates for statefulset workloads. 

### Goals

The goals of this proposal are:
1. Provide evaluation of a couple of approaches to statefulset support in Argo Rollouts.
2. Design support for statefulset workloads within Argo Rollouts. 
3. Support Canary and Blue/Green update strategy. 
 

### Non Goals

Any support for Stateful workloads should not reimplement the statefulset controller nor alter guarantees that the statefulset controller provides. 



For purpose of this proposal we identify two general types of applications deployed using statefulsets

#### Type 1 
Distributed databases such as postgres, consul, etc. These typically use a headless service. Pods connect directly to other pods. These workloads are quorum sensitive. Examples would be databases such as postgres or consul. 

#### Type 2
 Applications that use persistent storage but do not connect directly via a k8s service. Examples might include log aggregators. 


# Background 

## Argo Rollout plugins

Currently Argo Rollouts supports providing [plugins](https://argoproj.github.io/argo-rollouts/plugins/). These plugins can be referenced by canary steps in the Rollout spec. 



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

..this means that we need to consider how to track using labels

##### Pod management policy

Applies only to scaling operations for statefulsets. 

`Parallel` 

`OrderedReady`




##### Statefulset features 
RollingUpdate stategy supports adding a `maxUnavailable` field to ensure that rolling updates only result in 1 pod at a time. 
This feature is currently alpha as of 1.24 and does not seem slated for beta or stable support in the near future. 

 

https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/#maximum-unavailable-pods 

2. Parititioned rollouts

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
https://github.com/kubernetes/enhancements/tree/master/keps/sig-apps/961-maxunavailable-for-statefulset

### Metrics

### Traffic Management




Traffic is not always captured/processed using service mesh solutions such as Istio. 

1. Istio -- headless services
https://istio.io/latest/docs/ops/configuration/traffic-management/traffic-routing/#headless-services 


### Analysis

#### Health of a statefulset workload 

Due to the nature of the statefulset workload analysis of the health can include things such as whether or not the database was upgraded properly. 

Items such as whether or not quorum was lost must be considered. 




### Experiments




## Requirements

1. Support customizability of quorum parameters. 
2. Automatic Rollbacks of Statefulsets.  
3. 

# Considerations




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



Let's walk through how the stateful rollout controller will perform a canary rollout for a log aggregator service (such as [vector](https://github.com/vectordotdev/vector)) using Istio. This statefulset has 10 pods. In this scenario the users want to update the container image tag to a new version ie `image: docker.io/vector:0.40.0` to `image: docker.io/vector:0.42.1`. 

Below is the configuration of the Rollout.

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
  revisionHistoryLimit: 3
  strategy:
    canary: 
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
      - pause: {}
      - setWeight: 40
      - pause: {duration: 10}
      - setWeight: 60
      - pause: {duration: 10}
      - setWeight: 80
      - pause: {duration: 10}
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
    - route:
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

Step 1: update 10% of the pods with the new revision. This occurs via a rolling update partition. In this case the total # of pods is 20. So the rolling update partition value will be set to 17 which will allow for 2 pods to be deployed with the new revision. 

Set traffic weight to 20% to the canary revision. This will result in the rollouts controller updating the weight on the `VirtualService` to 20%. 






1. 



### Blue/Green

`blue-service`

`green-service`


### User stories

As a platform maintainer I would like to offer ways to safely upgrade statefulsets using blue/greeen and canary upgrade strategies. 

As a developer I would like to perform safe upgrades of statefulset services with automated rollbacks. 


### Open Questions

1. How can we suspend the StatefulSet such that we don't have to worry about git changes overriding rollouts. Rollouts currently uses the `spec.paused` field on Deployments to prevent the Deployment from creating new replicasets. What would be the corresponding way to do this with StatefulSets? 
  1a. a if we cannot achieve this with a paused parameter we may need to handle the creation of the statefulsets in the controller. 

2. 

https://github.com/kubernetes/enhancements/blob/master/keps/sig-apps/961-maxunavailable-for-statefulset/README.md
https://openkruise.io/docs/user-manuals/advancedstatefulset/

