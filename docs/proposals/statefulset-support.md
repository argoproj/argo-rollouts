---
title: Statefulset support
authors:
  - '@aburan28'
sponsors:
  - '@zaller'
creation-date: 2024-11-16
---

## Summary
Stateful workloads are currently un-supported in Argo Rollouts. 
For purpose of this proposal we identify two general types of applications deployed using statefulsets

1. Distributed databases such as postgres, consul, etc. These typically use a headless service  
2. Applications that use persistent storage. Examples vector log aggregator. 




## Motivation
Adding support will increase adoption and cover an important use-case. 

### Goals

The goals of this proposal are:
1. Design support for statefulset workloads within argo rollouts. 
2. 



### Non-Goals



### Background 

#### Statefulset workload
One reason statefulsets are used is that they provide a stable pod identity. This can be used to associate a parituclar pod with a PVC. 


##### Rolling Updates 

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



##### Headless service 
A big consideration with Argo rollout support is that traffic hits pods directly instead of hitting a k8s service. 

Implications include
1. 


##### Pod management policy

applies only to scaling operations for statefulsets. 

`Parallel` 

`OrderedReady`


#### Traffic Routing

https://istio.io/latest/docs/ops/configuration/traffic-management/traffic-routing/#headless-services 



## Proposal

1. Ideally 


### Use cases

1. 

### Implementation Details/Notes/Constraints

#### Configuration


#### Interface


### Security Considerations


### Risks and Mitigations

### Upgrade / Downgrade Strategy

## Drawbacks


## Alternatives


### User stories

As a platform maintainer I would like to offer ways to safely upgrade statefulsets using blue/greeen and canary upgrade strategies. 
