---
title: External Resources Plugin
authors:
  - '@aburan28'
sponsors:
  - '@zaller'
creation-date: 2025-01-18
---

## Summary

Currently Argo Rollouts only supports the `Deployment` workload type. There are a variety of other workloads that others would like to perform canary/blue-green upgrades on such as statefulsets or daemonsets. 


## Motivation  
In order to support other workloads in Rollouts besides Deployments a Rollout resource plugin would be ideal. 

The current `Rollouts` controller is heavily coupled to `ReplicaSets`. 

## User Stories
As a developer, it want to be able to define blue-green/canary rollouts for statefulsets/daemonsets. 

### Goals
- Allow open source community to implement rollouts for external resource types 
- Provide a path forward in argo rollouts for supporting statefulsets/daemonsets and other workload types. 


## Options
Below are two options and their high level summaries. 
### Option 1: RolloutsPlugin controller 
This would entail deploying a new dedicated controller that reconciles a new `RolloutPlugin` CRD. This would be essentially a greenfield implementation of the existing rollouts controller. It would be agnostic to all workload types to accomodate flexibility to workloads other than `PodSpec` based. 


### Option 2: Resource Plugin
Modify the existing rollouts controller and add to the spec of the Rollout a new `resourceCreation` plugin reference. 

## Option 1: RolloutsPlugin controller design  

Create a new controller for `RolloutsPlugin`.
Additionally add the following CRDs:
1. RolloutsPlugin
2. Revisions


```yaml
kind: RolloutsPlugin
metadata:
  name: statefulset-plugin
spec:
  selector:
    matchLabels:
      name: blahapp
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

```








### Option 2: Proposal
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
