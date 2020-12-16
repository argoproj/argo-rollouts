# Changelog

# v0.10.2

## Changes since v0.10.1

### Controller
* fix: switch pod restart to use evict API to honor PDBs
* fix: ephemeral metadata injection was dropping metadata injected by mutating webhooks
* fix: requiredForCompletion did not work for an experiment started by a rollout
* fix: Add missing RoleBinding file to namespace installation

# v0.10.1

## Changes since v0.10.0

### Controller
* fix: Correct Istio VirtualService immediately (#874)
* fix: restart was restarting too many pods when available > spec.replicas (#856)

### Plugin
* fix: plugin incorrectly treated v0.9 rollout as v0.10 when it had numeric observedGeneration (#875)

# v0.10.0

## Notable Features
* Ability to set canary vs. stable ephemeral metadata on rollout Pods during an update
* Support new metric providers: New Relic, Datadog
* Ability to control canary scale during an update
* Ability to restart up to maxUnavailable pods at a time for a canary rollout
* Ability to self reference rollout metadata as arguments to analysis
* Ability to fully promote blue-green and canary rollouts (skipping steps, analysis, pauses)
* kubectl-argo-rollouts plugin command to lint rollout
* kubectl-argo-rollouts plugin command to undo a rollout (same as kubectl rollout undo)

## Upgrade Notes

Rollouts v0.10 has switched to using Kubernetes [CRD Status Subresources](https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definitions/#status-subresource) ([PR #789](https://github.com/argoproj/argo-rollouts/pull/789)). This feature allows the rollout controller to record the numeric `metadata.generation` into `status.observedGeneration` which provides a reliable indicator of a Rollout who's spec has (or has not yet) been observed by the controller (for example if the argo-rollouts controller was down or delayed).

A consequence of this change, is that the v0.10 rollout controller should be used with the v0.10 kubectl-argo-rollouts plugin in order to perform actions such as abort, pause, promote. Similarly, Argo CD v1.8 should be with the v0.10 rollout controller when performing those same actions. Both kubectl-argo-rollouts plugin v0.10 and Argo CD v1.8 are backwards compatible with v0.9 rollouts controller.

## Changes since v0.9

### Controller

* feat: set canary/stable ephemeral metadata to pods during updates (#770)
* feat: add support for valueFrom in analysis arguments. (#797)
* feat: Adding rollout_info_replicas_desired metric. Fixes #748 (#749)
* feat: restart pods up to maxUnavailable at a time
* feat: add full rollout promotion (skip analysis, pause, steps) 
* feat: use CRD status subresource (#789)
* feat: Allow setting canary weight without side-effects. Fixes #556 (#677)
* fix: namespaced scoped controller support (#818)
* fix: fetch secrets on-demand to fix controller boot for large clusters (#829)

### Analysis
* feat: Add New Relic metricprovider (#772)
* feat: Add Datadog metric provider. Fixes #702 (#705)

### Plugin
* feat: Implement kubectl argo rollouts lint
* feat: Add undo command in kubectl plugin. Fixes #575 (#812)
* fix: kubectl plugin should use dynamic client

### Misc
* fix: rollout kustomize transform analysis ref should use templateName instead of name (#809)
* fix: add missing Service kustomize name reference in trafficRouting/alb/rootService (#699)


# v0.9.3

## Changes since v0.9.2

### Controller
* fix: scaleDownDelayRevisionLimit was off by one (#816)
* fix: background analysis refs were not verified. requeue InvalidSpec rollouts (#814)
* fix(controller): fix unhandled panic from malformed rollout (#801)
* fix(controller): validation should not consider privileged security context (#802)

# v0.9.2

## Changes since v0.9.1

### Controller
* fix(controller): controller did not honor maxUnavailable during rollback (#786)
* fix(controller): blue-green with analysis was broken (#780)
* fix(controller): blue-green fast-tracked rollbacks would still start analysis templates
* fix(controller): prePromotionAnalysis with previewReplicaCount would pause indefinitely w/o running analysis
* fix(controller): calculate available replicas from active ReplicaSet (#757)

### Plugin
* feat(plugin): indicate the stable ReplicaSet for blue-green rollouts in plugin
* feat(plugin): plugin now surfaces InvalidSpec errors and failed analysisrun messages (#729)
* fix(plugin): bluegreen scaleDownDelay was delaying Healthy status. Present errors in message field (#768)

# v0.9.1

## Changes since v0.9.0
### General
* feat: writeback rollout updates to informer to prevent stale data (#726)
* fix: unavailable stable RS was not scaled down to make room for canary (#739)
* fix: make controllers tolerant to spec marshalling errors (#666)
* perf: Create IstioVirtualServiceLister (#656)
* fix: add missing log message when a controller's syncHandler returns error (#658)
* fix: support azure auth (#664)

### Analysis
* feat: web metrics preserve data types, allow insecure tls, and make jsonPath optional (#731)
* fix: analysis controller could get into a hotloop with terminated run (#724)
* fix: do not create analysisruns with initial deploy (#722)
* fix: add Failed AnalysisRun phase status to analysis_run_metric_phase and analysis_run_phase metrics. (#618)

# v0.9.0

## Changes since v0.8
### General
* fix: Fix various panics #603
* feat: add security context to run as non-root #498
  
### Rollouts
* feat: Controller Validation #549
* feat: Controller Validation for objects referenced by Rollout #600
* feat: Add Rollout replicas metrics (#507) #581
* feat: Add support for rootService within ALB traffic routing #634

* fix: Populate .spec.template with default values before Rollout Validation #580
* fix: Add Rollout/scale to aggregate roles #637
* Fix: remove hash selector after switching from bg to canary #515
* fix: Set the currentStepIndex to max after bg to canary #558

### Traffic Routing
* feat: SMI TrafficSplit Support for Canary #520

### Kubectl Plugin
* feat: add shortened option -A for --all-namespaces #615

### Analysis
* feat: ClusterAnalysisTemplates (Cluster scoped AnalysisTemplates) #560
* feat: Uplevel AnalysisRun status to Rollout status #578

* fix: Modify arg verification to check ValueFrom #500
* fix: Fix analysis validation to include Kayenta #545

# v0.8.3

## Changes since v0.8.2
### General
* fix: Modify arg verification to check ValueFrom (#500)
* fix: remove hash selector after switching from bg to canary (#515)

# v0.8.2

## Changes since v0.8.1
### Rollouts
* fix: Ensure ALB action with weight 0 marshalls correctly (#493)
* fix: Add missing clusterrole for deleting pods (#490)

# v0.8.1

## Changes since v0.8.0
### General
* fix: Remove validation for limits and requests (#480)

### Rollouts
* fix: Duplicate StableRS to canary.StableRS (#483)

### Kubectl plugin
* fix: Make kubectl plugin backwards compat with canary.stableRS (#482)

# v0.8.0

## Breaking Changes
* The metric `rollout_created_time` is being removed.
* The `.status.canary.stableRS` is being deprecated for `.status.stableRS`. This release has the code to handle the migration, and the Rollout spec will updated to remove `.status.stableRS` in a future release.

## Contributors
Thank you to the following contributors for their work in this release!
* cronik
* dthomson25
* duboisf
* jessesuen
* khhirani
* moensch
* nghialv
  
## Changes since v0.7.2
### General
* feat: Improve Prometheus metrics (#461)
* feat: Add metrics on queues and go client http calls (#416)
* feat: Add patchMergeKey and patchStrategy struct tags and comments (#386)
* feat: Improve removing k8s 1.18 fields (#436)
* fix: Reduce log from error to warning (#394)
* chore: Download go deps explicitly in Dockerfile (#464)
* chore: Standardize controller-gen to v0.2.5 (#431)
* chore: Migrate from dep to go modules (#331)
* chore: Add auto generated sites/ to gitignore (#398)
* docs: Add remote name to 'make release-docs' (#435)
* docs: Documentation cleanup (#437)
* docs: Add Go mod download command to contributor docs (#425)
* docs: Corrected HPA doc (#396)
* docs: Remove extra comma in docs
* docs: Update README.md (#411)

### Rollouts
* feat: Introduce Anti-Affinity option to rollout strategies (#445)
* feat: Add ability to restart Pods (#453)
* feat: Add ALB Ingress controller support (#444)
* feat: Add Nginx canary traffic management (#426)
* feat: Add BlueGreen Pre Promotion Analysis (#415)
* feat: Add BlueGreen Post Promotion Analysis (#442)
* feat: Allow Rollout to specify multiples templates (#409)
* feat: Make pause duration as string with time unit (#423)
* feat: Use managed-by annotation (#448)
* refactor: Refactor BlueGreen Strategy (#388)
* fix: Update Role/ClusterRole for Ingress access (#439)
* fix: rollout transformer for pod affinity. add new v0.7 name references and testing (#399)
* chore: Add StableRS to rollout status (#441)
* chore: Fix wrong comment about the formula of calculating the replica number (#447)

### Analysis
* feat: Improve wavefront provider (#465)
* feat: Allow AnalysisTemplates to reference secrets (#420)
* improvement: Surface failure reasons for Rollouts/AnalysisRuns (#434)
* refactor: Perform arg substitution in Analysis controller (#407)
* docs: Use correct podTemplateHashValue attribute for valueFrom (#417)
* docs: Update web metrics section (#381)
* docs: Use correct magic value in Analysis docs (#378)

### Experiments
* feat: Experiments passed duration succeed with running analysis (#392)
* feat: Allow ex to use availableAt and finishedAt as args (#400)
* refactor: Refactor Experiment handling of pod hashes (#385)

### Kubectl plugin
* feat: Show scale down time for Blue Green ReplicaSets (#370) (#382)
* feat: Add more command aliases in kubectl plugin (#414)
* chore: Set kubectl flags on root command (#456)
* docs: Generate kubectl plugin docs (#422)
* docs: Plugin command enhancements (#454)

# v0.7.2

## Changes since v0.7.1
### Rollouts
* Update RS if RS's annotations need to be changed #413

# v0.7.1

## Changes since v0.7.0

### General
* Adding ca-certificates to docker image (#393)
* Add patchMergeKey and patchStrategy struct tags and comments (#386)
* Reduce log from error to warning (#394)

### Experiments
* Allow ex to use availableAt and finishedAt as args (#400)
* Experiments passed duration succeed with running analysis (#392)
* Refactor Experiment handling of pod hashes (#385)

# v0.7.0

## Important Notices
- Please upgrade to v0.6.x before upgrading to v0.7. Pre v0.6.0 has a different pausing logic, and v0.7.0 removes the depreciated PauseStartTime field. The v0.6.x versions have a migration script that is removed in v0.7.0. 
- This release introduces an alpha implementation of Rollouts leveraging Istio resources for traffic shaping. Check out [traffic management](https://argoproj.github.io/argo-rollouts/features/traffic-management/) for more info.

## Changes since v0.6.3

### General
* Support instance ids for rollout controller segregation #342
* Remove PauseStartTime #349
* Vendor mockery utility #347
* Remove loud log message #333

### Rollouts
#### General
* Add stableService field #337

#### Traffic Routing
* Initial Istio implementation #341
* Implement watch for Istio resources #354
* Add validation to istio virtual services #355

### Kubectl Plugin
* Introduce 'kubectl argo rollouts terminate' command #297

### Analysis

#### General
* Allow controller to delay analysis #350
* Create one background analysis per revision #309
* Allow AnalysisRun to complete an experiment #345

#### Providers
* Wavefront metric provider #338
* Web metric provider #318
* Refactor common logic in providers to library #368
* Allow web provider to be parameterized #368

# v0.6.3

## Changes since v0.6.2
### Bug Fixes

* Fix premature scaledown (#365)
* Add namespace restriction to job informer (#362)
* Fix honoring autoPromotionSeconds (#360)
* Ensure podHash stays on stable-svc selector (#340)

# v0.6.2

## Changes since v0.6.1
### Bug Fixes

* omitted revisionHistoryLimit was not defaulting to 10 (#330)
* Fix panic if rollout cannot create a new RS (#328)
* Enable controller to handle panics with crashing (#328)

# v0.6.1

## Changes since v0.6.0
### Bug Fixes

- Create one background analysis per revision (#309)
- Fix Infinite loop with PreviewReplicaCount set (#308)
- Fix a delete by zero in get command (#310)
- Set StableRS hash to current if replicaset does not actually exist (#320) 
- Bluegreen: allow preview service/replica sets to be replaced and fix sg fault in syncReplicasOnly (#314)

# v0.6.0

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
- Define explicit args in AnalysisTemplates and simplify AnalysisRun spec (##283)
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

## Changes since v0.4.1
#### Bug Fixes
- Honor MaxSurge and MaxUnavailable after last step (##141)
- Fix maxSurge maxUnavailable zero check (##135)
- Add .Spec.Replicas if not set in rollouts (##125)

# v0.4.1

## Changes since v0.4.0
#### Bug Fixes
- Workaround K8s inability to properly validate 'items' subfields ##114

# v0.4.0

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

## Important BlueGreen Strategy Change
In v0.4.0, Argo Rollouts will have a breaking change where we will only pause BlueGreen rollouts if they have a new field called `autoPromotionEnabled` under the `spec.strategy.blueGreen` set to false. If the field is not listed, the default value will be true, and the rollout will immediately promote the new Rollout. This change was introduced to address https://github.com/argoproj/argo-rollouts/issues/80. 

To prepare for v0.4.0, v0.3.2 will introduce the `autoPromotionEnabled` field, but the controller will not act on the field. As a result, you can add the `autoPromotionEnabled` field without breaking your existing rollouts.

## Enhancements
- Add autoPromotionEnabled with no behavior change 
## Fixes
- Fix controller crash caused by glog attempting to write to /tmp (##94)

# v0.3.1

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
