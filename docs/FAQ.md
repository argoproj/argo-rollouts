# FAQ

## General

### Does Argo Rollouts depend on Argo CD or any other Argo project?

Argo Rollouts is a standalone project. Even though it works great with Argo CD and other Argo projects, it can by used
on its own for Progressive Delivery scenarios. More specifically, argo Rollouts does **NOT** require that you also have installed Argo CD on the same cluster.

### How does Argo Rollouts integrate with Argo CD?
Argo CD understands the health of Argo Rollouts resources via Argo CD’s [Lua health check](https://github.com/argoproj/argo-cd/blob/master/docs/operator-manual/health.md). These Health checks understand when the Argo Rollout objects are Progressing, Suspended, Degraded, or Healthy.  Additionally, Argo CD has Lua based Resource Actions that can mutate an Argo Rollouts resource (i.e. unpause a Rollout).

As a result, an operator can build automation to react to the states of the Argo Rollouts resources. For example, if a Rollout created by Argo CD is paused, Argo CD detects that and marks the Application as suspended. Once the new version is verified to be good, the operator can use Argo CD’s resume resource action to unpause the Rollout so it can continue to make progress. 

### Can we run the Argo Rollouts kubectl plugin commands via Argo CD?
Argo CD supports running Lua scripts to modify resource kinds (i.e. suspending a CronJob by setting the `.spec.suspend` to true). These Lua Scripts can be configured in the argocd-cm ConfigMap or upstreamed to the Argo CD's [resource_customizations](https://github.com/argoproj/argo-cd/tree/master/resource_customizations) directory. These custom actions have two Lua scripts: one to modify the said resource and another to detect if the action can be executed (i.e. A user should not be able to resuming a unpaused Rollout). Argo CD allows users to execute these actions via the UI or CLI.

In the CLI, a user (or a CI system) can run
```bash
argocd app actions run <APP_NAME> <ACTION> 
```
This command executes the action listed on the application listed.

In the UI, a user can click the hamburger button of a resource and the available actions will appear in a couple of seconds. The user can click and confirm that action to execute it.

Currently, the Rollout action has two available custom actions in Argo CD: resume and restart.
* Resume unpauses a Rollout with a PauseCondition
* Restart: Sets the RestartAt and causes all the pods to be restarted.

### Does Argo Rollout require a Service Mesh like Istio?
Argo Rollouts does not require a service mesh or ingress controller to be used. In the absence of a traffic routing provider, Argo Rollouts manages the replica counts of the canary/stable ReplicaSets to achieve the desired canary weights. Normal Kubernetes Service routing (via kube-proxy) is used to split traffic between the ReplicaSets. 

### Does Argo Rollout require we follow GitOps in my organization?

Argo Rollouts is a Kubernetes controller that will react to any manifest change regardless of how the manifest was changed. The manifest can be changed
by a Git commit, an API call, another controller or even a manual `kubectl` command. You can use Argo Rollouts with any traditional CI/CD
solution that does not follow the GitOps approach.

## Rollouts

### Which deployment strategies does Argo Rollouts support?
Argo Rollouts supports BlueGreen, Canary, and Rolling Update. Additionally, Progressive Delivery features can be enabled on top of the blue-green/canary update, which further provides advanced deployment such as automated analysis and rollback.

### Does the Rollout object follow the provided strategy when it is first created?
As with Deployments, Rollouts does not follow the strategy parameters on the initial deploy. The controller tries to get the Rollout into a steady state as fast as possible. The controller tries to get the Rollout into a steady state as fast as possible by creating a fully scaled up ReplicaSet from the provided `.spec.template`. Once the Rollout has a stable ReplicaSet to transition from, the controller starts using the provided strategy to transition the previous ReplicaSet to the desired ReplicaSet.

### How does BlueGreen rollback work?
A BlueGreen Rollout keeps the old ReplicaSet up and running for 30 seconds or the value of the scaleDownDelaySeconds. The controller tracks the remaining time before scaling down by adding an annotation called `argo-rollouts.argoproj.io/scale-down-deadline` to the old ReplicaSet. If the user applies the old Rollout manifest before the old ReplicaSet before it scales down, the controller does something called a fast rollback. The controller immediately switches the active service’s selector back to the old ReplicaSet’s rollout-pod-template-hash and removes the scaled down annotation from that ReplicaSet. The controller does not do any of the normal operations when trying to introduce a new version since it is trying to revert as fast as possible. A non-fast-track rollback occurs when the scale down annotation has past and the old ReplicaSet has been scaled down. In this case, the Rollout treats the ReplicaSet like any other new ReplicaSet and follows the usual procedure for deploying a new ReplicaSet.

### What is the `argo-rollouts.argoproj.io/managed-by-rollouts` annotation?
Argo Rollouts adds an `argo-rollouts.argoproj.io/managed-by-rollouts` annotation to Services and Ingresses that the controller modifies. They are used when the Rollout managing these resources is deleted and the controller tries to revert them back into their previous state.

## Experiments

### Why doesn't my Experiment end?
An Experiment’s duration is controlled by the `.spec.duration` field and the analyses created for the Experiment. The `.spec.duration` indicates how long the ReplicaSets created by the Experiment should run. Once the duration passes, the experiment scales down the ReplicaSets it created and marks the AnalysisRuns successful unless the `requiredForCompletion` field is used in the Experiment. If enabled, the ReplicaSets are still scaled-down, but the Experiment does not finish until the Analysis Run finishes.

Additionally, the `.spec.duration` is an optional field. If it’s left unset, and the Experiment creates no AnalysisRuns, the ReplicaSets run indefinitely. The Experiment creates AnalysisRuns without the `requiredForCompletion` field, the Experiment fails only when the AnalysisRun created fails or errors out. If the `requiredForCompletion` field is set, the Experiment only marks itself as Successful and scales down the created ReplicaSets when the AnalysisRun finishes Successfully.

Additionally, an Experiment ends if the `.spec.terminate` field is set to true regardless of the state of the Experiment.

## Analysis
### Why doesn't my AnalysisRun end?
The AnalysisRun’s duration is controlled by the metrics specified. Each Metric can specify an interval, count, and various limits (ConsecutiveErrorLimit, InconclusiveLimit, FailureLimit). If the interval is omitted, the AnalysisRun takes a single measurement. The count indicates how many measurements should be taken and causes the AnalysisRun to run indefinitely if omitted. The ConsecutiveErrorLimit, InconclusiveLimit, and FailureLimit define the thresholds allowed before putting the rollout into a completed state.

Additionally, an AnalysisRun ends if the `.spec.terminate` field is set to true regardless of the state of the AnalysisRun.

### What is the difference between failures and errors?
Failures are when the failure condition evaluates to true or an AnalysisRun without a failure condition evaluates the success condition to false. Errors are when the controller has any kind of issue with taking a measurement (i.e. invalid Prometheus URL).

