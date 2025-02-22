---
title: External Resources Plugin
authors:
  - '@aburan28'
sponsors:
  - '@zaller'
creation-date: 2025-01-18
---

## Summary
Currently, Argo Rollouts supports only the Deployment workload type. Many users, however, need to perform canary or blue-green upgrades on other workloads such as statefulsets and daemonsets. This proposal outlines an approach to enable support for external resource types through a plugin-based architecture.

## Motivation
The current Rollouts controller is tightly coupled to ReplicaSets, which limits support to Deployment workloads only. There is a clear need to extend functionality so that other workload types—like statefulsets and daemonsets—can also benefit from advanced rollout strategies without requiring major rewrites each time a new resource type is added.



## User Stories
As a developer, I want to define blue-green and canary rollouts for workload types such as statefulsets and daemonsets, so that I can manage their deployment strategies consistently.


## Goals
- Allow open source community to implement rollouts for external resource types 
- Provide a path forward in argo rollouts for supporting statefulsets/daemonsets and other workload types. 

## Non-Goals
- re-implementing other non deployment controllers (ie StatefulSet, DaemonSet) in the rollouts controller
- modifification of controllers outside of rollouts controller

## Options
Below are two options and their high level summaries. 

### RolloutsPlugin controller 
Review of the `Rollouts` controller shows that the existing `Rollouts` controller is highly coupled to replicasets. Modification of that controller to accept other workloads is significant. A new dedicated controller would be created that reconciles a new `RolloutPlugin` CRD. This would be essentially a greenfield implementation of the existing rollouts controller. It would be agnostic to all workload types to accomodate flexibility to workloads other than `PodSpec` based. 

Currently the `Rollouts` [CRD](https://argo-rollouts.readthedocs.io/en/stable/features/specification/) includes a section to include a template PodSpec which effectively couples the `Rollouts` CRD to the core PodSpec of kubernetes. While changes are not frequent, they do happen and require updates to the Rollouts CRD to include such changes. 

Ideally this can be decoupled from the `Rollouts` CRD entirely and in this `RolloutsPlugin` controller design, it will reference a PodSpec from elsewhere. 

As mentioned in the preceding sections a non-goal of this controller is to re-implement custom logic of controllers. 




```yaml
apiVersion: argorollouts.io/v1alpha1
kind: RolloutsPlugin
metadata:
  name: statefulset-plugin
spec:
  workloadRef:
    apiVersion: 
    name: statefulset
    kind: 
  strategy:
    canary:
    blueGreen:

```


```yaml
apiVersion: argorollouts.io/v1alpha1
kind: Revision
metadata:
  name: rev0303
	labels:
		<retrieve labels from other resource>
spec:

status:
  conditions:
    - 

```

#### Examples
Below are several high-level overviews of how the `RolloutsPlugin` would handle the rollout of each the following workloads. 

##### StatefulSet 


##### DaemonSet

##### Knative serving



The primary goal of the `RolloutsPlugin` controller is to support custom logic for deploying applications using blue/green or canary strategies. The developer will need to implement the following methods. 

```go

type CanaryStrategy interface {
  SetWeight()
  SetCanaryScale()
  SetMirrorWeight()
  Pause()
}

```









### Rollouts Resource Plugin
Modify the existing rollouts controller and add to the existing spec of the `Rollout` a new `resourceCreation` plugin reference. 

There exist several other plugin types within the Argo Rollouts codebase such as `stepPlugins`, `metricsPlugins`, and `trafficRouting` plugins. 
This implementation would follow in those plugin footsteps and take the same approach. 
A resource plugin would be responsible for the full lifecycle of the external resources. For example if using a resource plugin that manages statefulsets, the plugin should handle creation, updates, deletes, and rollbacks of the statefulset. 


```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: statefulset
  labels:
    blah: blah
  annotations:
    blah: balh
spec:
  resourcePlugin:
    name: statefulset-plugin


  template:
    metadata:
      labels:
        pod: a
      annotations:
        pod: a

    spec:
      containers:
        - name: a

    strategy:
      
      


```


```yaml

status:
  canary:
    resourcePluginStatus:
      - index: 1
        name: statefulset
        message: Call completed with status code 302
        phase: Successful
        startedAt: '2024-02-15T20:05:40Z'
        finishedAt: '2024-02-15T20:21:40Z'
        status: {}
```



#### Resource Plugin Interface

```go
type Phase string

const (
	PhaseRunning      Phase = "Running"
	PhaseSuccessful   Phase = "Successful"
	PhaseFailed       Phase = "Failed"
	PhaseError        Phase = "Error"
)

type ResourceContext struct {
	PluginName   string
	Config       map[string]interface{}
	Status       map[string]interface{}
}

type ResourceStatus struct {
  Index         int
  Name          string
  Phase         Phase
  Message       string
  StartedAt     Time
  FinishedAt    Time
  Status        map[string]interface{}
}

type ResourceResult struct {
  Phase         Phase
  Message       string
  RequeueAfter  Duration
  Status        map[string]interface{}
}

type ResourcePlugin interface {
  Init() error
  Create(rollout) error 
  Update(rollout) error
  Delete(rollout) error
  Rollback(rollout) error
}

```


### Detailed execution flow

1. Initialization of plugin. Load the binary


2. Before reconciling the Rollout the controller will first check for the resource creation plugin. This will use a new struct `resourceContext`. If the resource plugin is running the 



3. 



#### Rollout Spec Updates

The Rollout spec will need to include a `resourcePlugin` section and a `podTemplate` reference for where to locate the spec for the pods. This could just use the existing `workloadRef` section.

```yaml
spec:
  volumeClaimTemplates: <?>

```

#### Example implementation for statefulsets

```yaml
apiVersion: 
kind: Rollout
metadata:
  name: sts
spec:
  resourcePlugin:
    name: statefulset

```

##### Questions
1. Can a Rollout have more than one resource creation plugin?
2. Do we add PodSpec fields used in Statefulsets such as `volumeClaimTemplates` to the Rollout spec?
3. How will this interact with the other plugin types?



#### Decisions
Rollouts controller will not handle the creation of external resources such as statefulsets. This will be entirely on the plugin implementation. 



### Alternatives considered

#### 1. support other resources in-tree
Support additional workloads within the existing codebase. This would require signficant refactoring of the existing codebase each time a new resource is added. 


#### 2. Rollout Plugin controller 
Create a dedicated rollout plugin controller with a corresponding `RolloutPlugin` CRD. 
