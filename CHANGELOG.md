# Changelog
# v0.6.1
## Quick Start
kubectl create namespace argo-rollouts
kubectl apply -n argo-rollouts -f https://raw.githubusercontent.com/argoproj/argo-rollouts/v0.6.1/manifests/install.yaml

# Changes since v0.6.0
## Bug Fixes

- Create one background analysis per revision (#309)
- Fix Infinite loop with PreviewReplicaCount set (#308)
- Fix a delete by zero in get command (#310)
- Set StableRS hash to current if replicaset does not actually exist (#320) 
- Bluegreen: allow preview service/replica sets to be replaced and fix sg fault in syncReplicasOnly (#314)

# v0.6.0
## Quick Start
```
kubectl create namespace argo-rollouts
kubectl apply -n argo-rollouts -f https://raw.githubusercontent.com/argoproj/argo-rollouts/v0.6.0/manifests/install.yaml
```

## Important Notices
- The pause functionality was reworked in the v0.6 release. Previously, the `.spec.paused` field was used by the controller to pause rollouts. However, this was an issue for users who wanted to manually pause the rollout since the controller assumed it was the only entity that set the field. In v0.6, the controller will add a pause condition to the `.status.pauseCondition` to pause a controller instead of setting `spec.paused`. The pause condition has a start time and a reason explaining why it paused. This allows users to set the `spec.paused` field manually and let the controller respect that pause. The v0.6 controller has a migration function to convert pre v0.6 rollouts to the new pause condition. The migration function will be removed in a future release.
- In pre-v0.6 versions, the BlueGreen strategy would have the preview service point at no ReplicaSets if the new ReplicaSet was receiving traffic from the active service. V0.6 changes that behavior to make the preview service always point at the latest ReplicaSet

## Changes since v0.5.0
#### General
- Update k8s library dependencies to v1.16 (##192)

#### Rollouts

###### Enhancements
- Add Rollout Context to reconciliation loop (##205)
- Refactor pausing (##211)
- Allow User pause (##216)
- Stop progress while paused (##193)
- Add pause condition migration (##229)
- Add abort functionality (##224)
- Rollout analysis plumbing (##183)
- Add AnalysisStep for Rollouts (##188)
- Add background analysis runs for rollouts (##196)
- Clean up old Background AnalysisRuns (##246)
- Clean up Experiments and AnalysisRuns (##197)
- Add initial Experiment Step (##165)
- Make specifying replicas/duration optional in the experiment step (##241)
- Terminate experiments from previous steps (##280)
- Add Analysis to RolloutExperimentStep (##238)
- Fix TimeOut check to consider experiment/analysis steps (##278)
- Pause a rollout upon inconclusive experiment (##256)
- Abort a rollout upon a failed experiment (##256)
- Add create AnalysisRun action in clusterrole (##231)

###### Bug Fixes
- Fix nil ptr for newRS (##233)
- Fix Rollout transformer config (##247)
- Always point preview service at the newRS (##217)
- Make active service required (##235)
- Reset ProgressDeadline on retry (##282)
- Ignore old running rs for RolloutCompleted status (##218)
- Remove scale down annot after scaling down (##187)
- Renames golang field names for blueGreen/canary to eliminate two API violations (##206)

#### Experiments
Check out the [Experiment Docs](https://github.com/argoproj/argo-rollouts/blob/release-v0.6/docs/features/experiments.md) for more information.

###### Enhancements
- Refactor experiments to use a context object (##208)
- Allow selectors to be overwritten when starting experiments (##249)
- Simplify experiment replicaset names (##274)
- Integrate Experiments with Analysis (##210)

#### Bug Fixes
- Fix experiment enqueue logic (##239)
- Annotate instead of label experiment names in replicasets (##262)
- Fix issue where a replicaset name collision could cause hot loop (##236)

#### Analysis
Check out the [Analysis Docs](https://github.com/argoproj/argo-rollouts/blob/release-v0.6/docs/features/analysis.md) for more information.
###### Enhancements
- AnalysisRun AnalysisTemplate Spec (##166)
- Initial analysis controller implementation (##168)
- Integrate analysis controller with provider interfaces (##171)
- Add metric knob for maxInconclusive (##181)
- Simplify provider interfaces to set error messages (##189)
- Implement ResumeAt logic (##232)
- Define explict args in AnalysisTemplates and simplify AnalysisRun spec (##283)
- Use a duration string instead of int to represent duration (##286)
- Truncate measurements when greater than default (10) (##191)
- Add counter for consecutiveError (##191)

###### Prometheus Provider
-  Add initial provider and Prometheus implementation (##170)
- Rename prometheus.server to address to better reflect API client interface (##207)
- Treat NaN as inconclusive on Prometheus provider (##275)

###### Job Provider
- Implement job-based metric provider (##186)
- Job metric argument substitution. Simplify metric provider interface (##268)

###### Kayenta Provider
- Initialize check in for kayenta metric provider ##284


#### Kubectl Plugin
Check out the [kubectl plugin docs](https://github.com/argoproj/argo-rollouts/blob/release-v0.6/docs/features/kubectl-plugin.md) for more information.

###### Enhancements
- Implement argo rollouts kubectl plugin (##195)
- Introduce `kubectl argo rollout list rollouts` command (##204)
- Introduce `kubectl argo rollout list experiments` command (##267)
- Introduce `kubectl argo rollout set image` command (##251)
- Introduce `kubectl argo rollout get` command (##230)
- Introduce `kubectl argo rollout promote` command (##277)
- Add ability to `kubectl argo rollouts set image *=myrepo/myimage` (##290)
- Add `get/retry experiment` commands. Support experiment retries (##263)
- Show running jobs as part of analysis runs (##278)
- Surface experiment images to CLI (##274)

# v0.5.0
## Quick Start
kubectl create namespace argo-rollouts
kubectl apply -n argo-rollouts -f https://raw.githubusercontent.com/argoproj/argo-rollouts/v0.5.0/manifests/install.yaml

## Changes since v0.4.2
#### Bug Fixes
- Rollout deletionTimestamp are not honored (##109)
- status.availableReplicas should not count old stacks (##143)
- Fix Infinitely loop on controller loop (##146)

#### Enhancements
- Fast rollback in BlueGreen during scale down period ##127
- Attach independent scaleDownDelays to older ReplicaSets ##145
- Add scaleDownDelayRevisionLimit to limit the number of old ReplicaSets scaled up ##129

#### Experiment CRD
This release of Argo Rollouts introduces the experiment CRD. The experiment CRD allows users to define multiple PodSpec's to run for a specific duration of time. This will help enable the Kayenta use-case where a user will need to start two versions of their application at the same time. Otherwise, the users cannot have an apples-to-apples comparison of these two versions as one will skew as a result of running for a longer period.

# v0.4.2
## Quick Start
kubectl create namespace argo-rollouts
kubectl apply -n argo-rollouts -f https://raw.githubusercontent.com/argoproj/argo-rollouts/v0.4.2/manifests/install.yaml

## Changes since v0.4.1
#### Bug Fixes
- Honor MaxSurge and MaxUnavailable after last step (##141)
- Fix maxSurge maxUnavailable zero check (##135)
- Add .Spec.Replicas if not set in rollouts (##125)

# v0.4.1
## Quick Start
kubectl create namespace argo-rollouts
kubectl apply -n argo-rollouts -f https://raw.githubusercontent.com/argoproj/argo-rollouts/v0.4.1/manifests/install.yaml

## Changes since v0.4.0
#### Bug Fixes
- Workaround K8s inability to properly validate 'items' subfields ##114

# v0.4.0
## Quick Start
kubectl create namespace argo-rollouts
kubectl apply -n argo-rollouts -f https://raw.githubusercontent.com/argoproj/argo-rollouts/v0.4.0/manifests/install.yaml

## Important Notes Before Upgrading
- For the BlueGreen strategy, Argo Rollouts will only pause rollouts that have the field `spec.strategy.blueGreen.autoPromotionEnabled` set to false. The default value of `autoPromotionEnabled` is true and causes the rollout to immediately promote the new version once it is available. This change was implemented to make the pausing behavior of rollouts more straight-forward and you can read more about it at ##80. Argo Rollouts v0.3.2 introduces the `autoPromotionEnabled` flag without making any behavior changes, and those behavior changes are enforced starting at v0.4.0. __In order to upgrade without any issues__, the operator should first upgrade to v0.3.2 and add the `autoPromotionEnabled` flag with the appropriate value.  Afterward, they will be safe to upgrade to v0.4.

- For the Canary Strategy, the Argo Rollouts controller stores a hash of the canary steps in the rollout status to be able to detect changes in steps. If the canary steps change during a progressing canary update, the controller will change the hash and restart the steps.  If the rollout is in a completed state, the controller will only update the hash. In v0.4.0, the controller changed how the hash of the steps was calculated, and you can read more about that at this issues: ##103.  As a result, __the operator should only upgrade Argo Rollouts to v0.4.0 when all the canary rollouts have executed all steps and have completed__. Otherwise, the controller will restart the steps it has executed.

## Changes since v0.3.2
#### Enhancements
- Add Ability to specify canaryService Service object to reach only canary pods ##91
- Simplify unintuitive automatic pause behavior for blue green strategy ##80 
- Add back service informer to handle Service recreations quicker ##71 
- Use lister instead of kubernetes api call to load service ##98
- Switch to controller-gen to generate crd with complete openapi validation spec ##84

#### Bug Fixes
- Change step hashing function to derive hash from json representation ##103
- CRD validation needs to be removed for resource requests/limits ##101
- Possible to exceed revisionHistoryLimit with canary strategy ##93
- Change in pod template hash after upgrading k8s dependencies ##88
- Controller is missing patch event privileges bug ##86
- Rollouts unprotected from invalid specs ##84 
- Fix logging fields ##97

# v0.3.2
## Quick Start
kubectl create namespace argo-rollouts
kubectl apply -n argo-rollouts -f https://raw.githubusercontent.com/argoproj/argo-rollouts/v0.3.2/manifests/install.yaml

## Important BlueGreen Strategy Change
In v0.4.0, Argo Rollouts will have a breaking change where we will only pause BlueGreen rollouts if they have a new field called `autoPromotionEnabled` under the `spec.strategy.blueGreen` set to false. If the field is not listed, the default value will be true, and the rollout will immediately promote the new Rollout. This change was introduced to address https://github.com/argoproj/argo-rollouts/issues/80. 

To prepare for v0.4.0, v0.3.2 will introduce the `autoPromotionEnabled` field, but the controller will not act on the field. As a result, you can add the `autoPromotionEnabled` field without breaking your existing rollouts.

## Enhancements
- Add autoPromotionEnabled with no behavior change 
## Fixes
- Fix controller crash caused by glog attempting to write to /tmp (##94)

# v0.3.1
## Quick Start
kubectl create namespace argo-rollouts
kubectl apply -n argo-rollouts -f https://raw.githubusercontent.com/argoproj/argo-rollouts/v0.3.1/manifests/install.yaml

## Breaking Changes
Rename autoPromoteActiveServiceDelaySeconds to autoPromotionSeconds ##77

## Changes since v0.3.0

#### Enhancements
* Switch to Scratch final image ##67

#### Fixes
* Enable fast Rollback in BlueGreen ##78 
* Respect ScaleDownDelay during non-happy path ##79
* Scale down older RS on non-happy path ##76
* Fix issue where pod template hash could be computed inconsistently ##75
* Cleanup replicasets in canary deployment ##73
* Don't requeue 404 errors ##72

# v0.3.0
## Quick Start
kubectl create namespace argo-rollouts
kubectl apply -n argo-rollouts -f https://raw.githubusercontent.com/argoproj/argo-rollouts/v0.3.0/manifests/install.yaml

## New Features
- HPA Support ##37
- Prometheus Metrics ##29 ##47 
- Introduce ProgressDeadlineSeconds ##54
- Improved Scalability ##45 

## Breaking Changes

The `status.verifyingPreview` field was depreciated and move to `spec.pause`.

#### BlueGreen Specific
- Add previewReplicaCount ##64
- Add ability to auto-promote active service ##59
- Add ScaleDownDelaySeconds ##57

## Changes since v0.2.2
#### Enchantments 
- HPA Support ##37
- Prometheus Metrics ##29
- Add previewReplicaCount ##64
- Add ProgressDeadlineSeconds ##54
- Add Invalid spec checks with regards to ProgressDeadlineSeconds ##62
- Improve eventing and metrics ##61
- Improve Available Condition ##60
- Convert Kustomize V1.0 to Kustomize v2.0 ##56
- Make Metrics port customizable ##55
- Replace gometalinter with golangci ##46
- Add support for gotestsum ##52
- Remove service informer ##45 
- Replace verifying preview with Paused ##43


#### Bug fixes
- Prevent early pause before svc change in BG ##51
- Fix aggregate roles naming collision with Argo Workflows  ##44
- Use recreate strategy for controller  ##44

# v0.2.2
Add missing events permissions to the clusterrole

# v0.2.1
Changes the following clusterroles to prevent name collision with Argo Workflows
- `argo-aggregate-to-admin` to `argo-rollouts-aggregate-to-admin`
- `argo-aggregate-to-edit` to `argo-rollouts-aggregate-to-edit`
- `argo-aggregate-to-view` to `argo-rollouts-aggregate-to-view`

# v0.2.0
- Implements the initial ReplicaSet-based Canary Strategy
- Cleans up Status fields
- Implicit understanding of rollback based on steps completion and pod hash for Blue Green and Canary

# v0.1.0
* Creates a controller that manages a rollout object that mimics a deployment object
* Declaratively offers a Blue Green Strategy by creating the replicaset from the spec and managing an active and preview service to point to the new replicaset
