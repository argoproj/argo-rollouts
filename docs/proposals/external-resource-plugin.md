---
title: Statefulset support
authors:
  - '@aburan28'
sponsors:
  - '@zaller'
creation-date: 2025-01-18
---

# Summary
In order to support other workloads in Rollouts besides Deployments a Rollout resource plugin would be ideal. 


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
    spec:
      


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
	Run(Rollout, ResourceContext) (ResourceResult, error)
	Terminate(Rollout, ResourceContext) (ResourceResult, error)
	Abort(Rollout, ResourceContext) (ResourceResult, error)
}

```

### Alternatives considered

#### 1. support other resources in-tree
Support additional workloads within the existing codebase. This would require signficant refactoring of the existing codebase each time a new resource is added. 


#### 2. Rollout Plugin controller 
Create a dedicated rollout plugin controller with a corresponding `RolloutPlugin` CRD. 
