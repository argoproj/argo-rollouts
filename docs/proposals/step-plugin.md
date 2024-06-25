---
title: Step Plugin
authors:
  - '@agaudreault'
sponsors:
  - '@zaller'
creation-date: 2024-03-27
---

# Step Plugin

Step plugins can be used to call code built outside of argo-rollout's codebase to execute actions during a canary rollout.

This document provides technical implementation proposals to avoid implementing rollout steps in Argo Rollout’s codebase.

## Summary

Rollout steps need to be implemented natively in Argo Rollout source code.
It makes it difficult for the community to add new rollout steps because their implementation
is coupled with Rollout release cycle. The Rollout maintainers also have to acquire
knowledge on the different technologies used in the steps and validate them on each release.

## Motivation

This section is for explicitly listing the motivation, goals and non-goals of this proposal.
Describe why the change is important and the benefits to users.

### Goals

The goals of this proposal are:

- Update steps outside Rollout’s release cycle
- Allow the community experts to maintain their steps
- Allow for a faster step development iteration
- Allow for more features without increasing complexity on the controller
- Allow users to use proprietary steps with Argo Rollouts

### Non-Goals

Implement plugins.

## Proposal

Rollout already has a plugin mechanism for metric providers and traffic routers as
documented in https://argoproj.github.io/argo-rollouts/plugins/. The implementation is based on [hashicorp/go-plugin](https://github.com/hashicorp/go-plugin).
This mechanism can be extended to support the steps plugin.

- Consistent with existing behavior.
- Add a `stepPluginStatuses` array to the `.status.canary` field.
- Users can consult the Rollout object after the execution to get details on their status.

### Use cases

More details in https://github.com/argoproj/argo-rollouts/issues/2685

### Implementation Details/Notes/Constraints

#### Configuration

The plugin will be configured alongside existing plugins.

```yaml
kind: ConfigMap
metadata:
  name: argo-rollouts-config
data:
  metricProviderPlugins: |-
    - name: "argoproj-labs/metrics"
      location: "file:///tmp/argo-rollouts/metric-plugin"
      args:
        - "--log-level"
        - "debug"
  stepPlugins: |-
    - name: "argoproj-labs/curl/v2"
      disabled: false
      location: "file:///tmp/argo-rollouts/step-plugin"
      sha256: "08f588b1c799a37bbe8d0fc74cc1b1492dd70abc"
      args:
        - "--log-level"
        - "debug"
```

#### Interface

_The interface implementation details related to go-plugin and rpc calls have been omitted for clarity._

```go
type Phase string

const (
	PhaseRunning      Phase = "Running"
	PhaseSuccessful   Phase = "Successful"
	PhaseFailed       Phase = "Failed"
	PhaseError        Phase = "Error"
)

type StepContext struct {
	PluginName   string
	Config       map[string]interface{}
	Status       map[string]interface{}
}

type StepStatus struct {
	Index         int
	Name          string
	Phase         Phase
	Message       string
	StartedAt     Time
	FinishedAt    Time
	Status        map[string]interface{}
}

type StepResult struct {
	Phase         Phase
	Message       string
  RequeueAfter  Duration
	Status        map[string]interface{}
}

type StepPlugin interface {
	Init() error
	Run(Rollout, StepContext) (StepResult, error)
	Terminate(Rollout, StepContext) (StepResult, error)
	Abort(Rollout, StepContext) (StepResult, error)
}
```

#### Rollout object (plugin input)

The step will provide rollout specific configuration defined by the users
as a map of key value pairs allowing the user to pass any declarative configs to the plugin.

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: example-rollout
spec:
  strategy:
    canary:
      ...
      steps:
        - setWeight: 20
        - plugin:
            name: argoproj/curl
            abortOnFailure: false    # <--- example
            progressDeadline: 30s    # <--- example
            config:
              url: https://example.com/
              some_key: some_value
        - pause: {}
        - setWeight: 40

```

#### Rollout Status (plugin output)

The controller will write a new object to the status.
This object will be used to persist the plugin state after the step execution and to allow other steps to use it.

```yaml
status:
  canary:
    stepPluginStatuses:
      - index: 2
        name: argoproj/curl
        message: Call completed with status code 302
        phase: Successful
        startedAt: '2024-02-15T20:05:40Z'
        finishedAt: '2024-02-15T20:21:40Z'
        status: {}
      - index: 4
        name: argoproj/async-task
        message: Waiting for result
        phase: Running
        startedAt: '2024-02-15T20:05:40Z'
        finishedAt: null
        status:
          id: 12
          path: /an/example/property
          validated: false
```

### Detailed execution flow

1. **At initialization**

   The plugins are loaded based on configuration and started as processes with the provided arguments in the configuration.
   The `Init()` method is called for plugins to perform one-time initialization such as creating clients and establishing connections.

2. **A rollout reach the plugin step for the first time**

   During the rollout, the controller will use the `currentStepIndex` to find the step to run.
   If the step is a plugin, the controller will create a StepContext object based
   on the configuration on the Rollout’s step object.

   The controller will try to retrieve the value of the current `stepPluginStatuses`,
   and if it matches the current step index, it will add the persisted state to the context.

   It will create a StepStatus object otherwise, set the name and index to the current step,
   set the startedAt value to the current time and the phase as Running.

3. **Running the step plugin**

   The plugin `Run()` method is called with the StepContext.
   The plugin implementation will use the StepContext to perform the necessary logic.

4. **Status is updated**

   Based on the return of the Run command, the StepStatus object is updated.
   Then, the StepStatus is assigned to the stepPluginStatuses property.

   The status is persisted in the object.

5. **Validate step completed**

   If the controller current step is of type plugin, the controller will check if the phase is successful,
   and if so, go to the next step.

   If the step is still running, the controller will requeue a reconcile operation based on the value of `RequeueAfter`.

   If the phase is failed, it will update the status and conditions, **aborting** the rollout.

#### Scenarios

##### Step plugin completes successfully

The `stepPluginStatuses` will not contain any object for the current step index, the plugin will perform the desired action successfully and return a successful state.

The state will be persisted and the controller wil execute the next step.

##### Step plugin completes with failed phase

If the step plugin returns a failed phase, the controller will set the rollout to **aborted** and persist the state.

The user will receive the feedback based on the Progressing condition status. This behavior is consistent with existing mechanisms aborting a rollout.

##### Step plugin completes with running phase

If the step plugin returns a running phase, the controller will persist the state, but will not increment the current step index.

The controller will requeue a reconcile operation based on the value of `RequeueAfter` and terminate the current reconciliation.

On the next reconciliation, the persisted state will be passed to the plugin `Run()` method.

##### Step plugin is called multiple times

If an external error happens causing the controller to crash after it called the `Run()` method and before it could persist the status,
the controller will replay the current plugin step, with a new context, like if it was the first time it is called.

> **A step plugin can be called multiple times and operations should be idempotent.**

##### Rollout is fully promoted during a step plugin

If a Rollout is forcefully considered fully promoted while the current step is in a Running phase,
the plugin will call the `Terminate()` method with the current context on the next reconciliation and update the status based on the result.

##### Rollout aborted during a step plugin

If a Rollout is aborted while the current step is in a Running phase, the plugin will call the `Abort()` method with the current context on the next reconciliation.
The controller will call the `Abort()` operation for each step that were executed in the reverse order.
The steps may or may not perform any action during the Abort.
The result of the abort will be saved in the status, overriding the state persisted during the `Run()`.
If the Abort operation has an error, the error is propagated to the controller.
The controller has the responsibility to retry the Abort operation, and eventually proceed with the next step if it never succeeds.

##### The step plugin reports an error

Before returning the error, the status is persisted with the error phase and message. Other properties of the current status remain unmodified so the step can be re-executed with the last known valid status.

For an expected retryable error, the plugin should return a Running phase with a RequeueAfter value to retry the execution.
For an expected un-retryable error, the plugin should return a Failed status.
After the state is persisted, the error is propagated to the controller and the controller error-handling logic will handle the error.

##### The step plugin uses Rollout information

The step plugin may need to have access to the current state of the Rollout. The step plugin will receive a deep copy of the rollout object in parameter.

##### I can investigate my step plugin execution

A user wants to know what happened during their custom step plugin after the execution. They can use the status in the Rollout object.

##### State is shared between plugins

A user wants to use an API to publish information about the rollout. The plugin first calls the API that returns a conversation ID.
Other steps need to use the conversationID during their execution.
The plugin step receives in parameters the full rollout object and the pluginName. This information can be used to retrieve the status of other plugin execution.

An utility function such as `PluginHelper.GetStatuses(rollout, pluginName)` can be implemented and made available to the plugins.

##### I want my rollout to continue event if my plugin failed

A parameter such as `abortOnFailure` can be added to the Rollout plugin step configuration object.
When specified, the controller can use the value to modify the default logic.

##### I dont want my plugin execution time to count towards the progress deadline

A parameter such as `ignoreProgressDeadline` can be added to the Rollout plugin step configuration object.
When specified, the controller can use the value to modify the default logic.

### Security Considerations

- Plugins binary can be validated with the configured `sha256`.

### Risks and Mitigations

- Rollout status size can grow more than expected based on plugins hygiene.
  - Object size can be validated with `unsafe.Sizeof(struct)` and a size limit can be imposed.
- Plugins that are failing or causing problems cannot be removed without updating all the Rollouts.
  - A `disabled` config can be added globally and ignore the plugin execution if true.

### Upgrade / Downgrade Strategy

It is expected that plugins will be compiled with different versions than the running argo-rollout controller. The plugins version could be either newer or older than the controller.

The hashicorp/go-plugin uses gob encoding with rpc.

> “The source and destination values/types need not correspond exactly. For structs, fields (identified by name) that are in the source but absent from the receiving variable will be ignored. Fields that are in the receiving variable but missing from the transmitted type or value will be ignored in the destination. If a field with the same name is present in both, their types must be compatible. Both the receiver and transmitter will do all necessary indirection and dereferencing to convert between gobs and actual Go values.” - package [encoding/gob](https://pkg.go.dev/encoding/gob)

Plugins should validate the objects they receive in parameters such as the `Rollout` and user confiuguration. If they expect a property to be set and it is not, it is highly probably that the controller's object version does not have that property.

For breaking changes, hashicorp/go-plugin has a `ProtocolVersion` property that can be used in the future.

Plugins can also be added with different names, which would require update to the Rollout CR objects as well.

## Drawbacks

- Gives a lot of power to the plugin and bad plugins could destabilize the rollouts
- If plugins need more permission, the access needs to be given using the Rollout service account.

## Alternatives

- Only implement vetted code in the argo-rollout codebase.
- Create a plugin that calls other containers
