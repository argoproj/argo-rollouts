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


For purpose of this proposal we identify two general types of applications deployed using statefulsets

1. Distributed databases such as postgres, consul, etc. These typically use a headless service  
2. Applications that use persistent storage. Examples vector log aggregator. 


## Motivation
Adding support will increase adoption and cover an important use-case. By providing rollout support for statefulsets developers can safely deploy updates for statefulset workloads. 

### Goals

The goals of this proposal are:
1. Provide evaluation of a couple of approaches to statefulset support in Argo Rollouts.
2. Design support for statefulset workloads within Argo Rollouts. 
3. Support Canary and Blue/Green update strategy. 
 

### Non Goals

Any support for Stateful workloads should not reimplement the statefulset controller nor alter guarantees that the statefulset controller provides. 



# Background 

## Argo Rollout plugins

Currently Argo Rollouts supports providing [plugins](https://argoproj.github.io/argo-rollouts/plugins/). These plugins can be referenced by canary steps in the Rollout spec. 



#### Statefulset workload
One reason statefulsets are used is that they provide a stable pod identity. This can be used to associate a parituclar pod with a PVC. s


##### Rolling Updates 

There are two strategies for statefulsets

1. `OnDelete` -- This updates the statefulset pods by requiring manual user intervention in order to delete the old pods. New pods will come up with the new version. 
2. `RollingUpdate` -- this is the default. 

##### Problems

1. Statefulset updates are exceptionally slow due to the ordered guarantees. Updates occur with 1 pod at a time. 




##### Headless service 
A big consideration with Argo rollout support is that traffic hits pods directly instead of hitting a k8s service. 

Implications include
1. 


##### Pod management policy

applies only to scaling operations for statefulsets. 

`Parallel` 

`OrderedReady`




##### Statefulset features 
RollingUpdate stategy supports adding a `maxUnavailable` field to ensure that rolling updates only result in 1 pod at a time. 
This feature is currently alpha as of 1.24 and does not seem slated for beta or stable support in the near future. 

 

https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/#maximum-unavailable-pods 

2. Parititioned rollouts

As of kubernetes 1.31 there is support for partitioned rolling updates https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/#partitions 
This allows developers to define behavior on statefulset updates using the ordinal index. 
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

1. Implement a step plugin for statefulsets. 
2. Implement a dedicated StatefulRollout CRD and StatefulRollout controller
3. Extend the existing Rollout controller to handle other workloads such as Statefulsets. 


#

## Proposal









### Design

The rollout controller at this time is opinionated about the type of workload it is meant to handle. While the rollout CRD has a field that references a `workloadRef` which takes an arbitrary `apiVersion`, `kind`, and `name`. Ideally this can serve as an entrypoint for a variety of other workloads such as Statefulsets. 




A big part of this is that the rollout controller needs to remove several of the kubernetes deployment/replicaset isms. Within the Rollout CRD there are several fields that reference or are opinioated about ReplicaSets/Deployments. 

For example:

```yaml
spec:
  strategy:
    canary: 
      ...
      maxUnavailable: 1
      maxSurge: '20%'
      minPodsPerReplicaSet: 2
```




```yaml


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
    name: distributed-database-workload
  
  


```



## Statefulset Rollout Walkthrough 

Let's walk through how the stateful rollout controller will perform a rollout for a log aggregator service. 

### Canary 

Updates a statefulset 





### Blue/Green

`blue-service`
`green-service`





### User stories

As a platform maintainer I would like to offer ways to safely upgrade statefulsets using blue/greeen and canary upgrade strategies. 




https://github.com/kubernetes/enhancements/blob/master/keps/sig-apps/961-maxunavailable-for-statefulset/README.md



https://openkruise.io/docs/user-manuals/advancedstatefulset/