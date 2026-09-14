# Changelog

# v1.1.0

## Notable Features
* Rollout Notifications
* Dynamic scaling of stable ReplicaSet (dynamicStableScale)
* Automated rollbacks without analysis (progressDeadlineAbort)
* Kustomize Open API Schema
* Rollout Dashboard as a Service
* Controlling Scaledown Behavior During Aborts (abortScaleDownDelaySeconds)
* Analysis: AWS CloudWatch Metric Provider
* AWS TargetGroup IP Verification
* Weighted Experiment Canary Steps
* Istio: Multicluster Support
* Istio: TLS Route Support
* Istio: Multiple VirtualServices
* AnalysisRun GC
* Analysis: Graphite Metric Provider

## Changes since v1.0

### Controller
* feat: support dynamic scaling of stable ReplicaSet as inverse of canary weight (#1430)
* fix: promote nil pointer error when there are no steps (#1510)
* feat: support management of multiple Istio VirtualService objects (#1381)
* feat: verify AWS TargetGroup after updating active/stable services (#1348)
* feat: ALB TrafficRouting with experiment step
* feat: TrafficRouting SMI with Experiment Step in Canary (#1351)
* feat: ability to abort an update when exceeding progressDeadlineSeconds (#1397)
* feat: add support for Istio VirtualService spec.tls[] (#1380)
* feat: configurable and more aggressive cleanup of old AnalysisRuns and Experiments (#1342)
* feat: ability to auto-create Services for each template in an Experiment (#1158)
* feat: introduce abortScaleDownDelaySeconds to control scale down of preview/canary upon abort (#1160)
* feat: argo rollout compatibility with emissary and edge stack v2.0 (#1330)
* feat: Add support for Istio multicluster (#1274)
* feat: add workload-ref/generation to rollout (#1198)
* feat: support notifications on rollout events using notifications-engine (#1175)
* chore: add liveness and readiness probe to the install manifests (#1324)
* fix: Nginx ingressClassName passed to canary ingress (#1448)
* fix: canary scaledown event could violate maxUnavailable (#1429)
* fix: analysis runs to wait for all metrics to complete (#1407)
* fix: Promote full did not work against BlueGreen with previewReplicaCount (#1384)
* fix: retarget blue-green previewService before scaling up preview ReplicaSet (#1368)
* fix: zero-value abortScaleDownDelay was not honored with setCanaryScale (#1375)
* fix: abort scaledown stable RS for canary with traffic routing (#1331)

### Analysis
* feat: add support for Graphite metrics provider (#1406)
* feat: Support CloudWatch as a metric provider (#1338)
* fix: Analysis argument validation (#1412)

### Plugin
* feat: create windows version for CLI (#1517)
* feat: provide shell completion. Closes #619 (#1478)
* fix: create analysisrun cmd using template generated name (#1471)
* fix: nil pointer in create analysisrun cmd (#1399)
* fix: lint subcommand for workload ref rollout (#1328)
* fix: undo referenced object for workloadRef rollout (#1275)

### Dashboard
* feat: allow selection of namespace in rollout dashboard (#1291)
* fix(ui): UI crashes on rollout view due to undefined status (#1287)

### Misc
* feat: kustomize rollout: add openapi to doc and examples (#1371)
* feat: add rollout stat row to grafana dashboard (#1343)

## Upgrade Notes
### Difference in scale down behavior during aborts

The v1.1 `abortScaleDownDelaySeconds` feature now allows users full control over the scaling
behavior of the canary/preview ReplicaSet during an abort. Previously in v1.0, it was not possible
to affect this behavior. As part of this feature, v1.1 also fixes some inconsistencies in behavior
with respect to abort scale down.

The most notable change is that upon an abort, the blue-green preview ReplicaSet in v1.1 will now
scale down 30 seconds after the abort, whereas in v1.0 the preview ReplicaSet was left running
indefinitely (without any option to scale it down). If you prefer the v1.0 behavior, you can set
`abortScaleDownDelaySeconds: 0`, which will leave the preview ReplicaSet running indefinitely
on abort:

```yaml
spec:
  strategy:
     blueGreen:
       abortScaleDownDelaySeconds: 0
```

Please read the full
[documentation](https://argoproj.github.io/argo-rollouts/features/scaledown-aborted-rs/) to understand
the differences in canary/preview scaling behavior for aborted Rollouts from v1.0 to v1.1.

# v1.0.6
## Changes since v1.0.4

### Bug Fixes

- fix: replica count for new deployment (#1449)
- fix: Nginx ingressClassName passed to canary ingress (#1448)
- fix: Analysis argument validation (#1412)
- fix: retarget blue-green previewService before scaling up preview ReplicaSet (#1368)
- fix: analysis runs to wait for all metrics to complete (#1407)
- fix: canary scaledown event could violate maxUnavailable (#1429)
- chore: release workflow docker build context should use local path and not git context (#1388)
- chore: github release action was using incorect docker cache (#1387)

# v1.0.4
## Changes since v1.0.3

### Controller
* fix: Promote full did not work against BlueGreen with previewReplicaCount

# v1.0.3

## Changes since v1.0.2

### Controller

* fix: nil pointer dereference when reconciling paused blue-green rollout (#1378)
* fix: Abort rollout doesn't remove all canary pods for setCanaryScale (#1352)
* fix: unsolicited rollout after upgrade from v0.10->v1.0 when pod was using service account (#1367)
* fix: default replica before resolving workloadRef (#1304)

# v1.0.2

## Changes since v1.0.1

### Controller

* feat: allow VirtualService HTTPRoute to be inferred if there is single route (#1273)
* fix: rollout paused longer than progressDeadlineSeconds would briefly degrade (#1268)
* fix: controller would drop fields when updating DestinationRules (#1253)
* fix: the wrong panel title on the sample dashboard (#1260)
* fix: analysis with multiple metrics (#1261)
* fix: Mitigate the bug where items are re-added constantly to the workqueue. #1193 (#1243)
* fix: workload rollout spec is invalid template is not empty (#1224)
* fix: Fix error check in validation for AnalysisTemplates not found (#1249)
* fix: make function call consistent with otherRSs definition (#1171)

### Plugin

* fix: avoid using root user in plugin container (#1256)


# v1.0.1

## Changes since v1.0.0

### Controller

* feat: WebMetric to support string body responses (#1212)
* fix: Modify validation to check Analysis args passed through RO spec (#1215)
* fix: AnalysisRun args could not be resolved from secret (#1213)


# v1.0.0

## Notable Features
* New Argo Rollouts UI available in `kubectl argo rollouts dashboard`
* Ability to reference existing Deployment workloads instead of inlining a PodTemplate at spec.template
* Richer prometheus stats and Kubernetes events
* Support for Ambassador as a canary traffic router
* Support canarying using Istio DestinationRule subsets

## Upgrade Notes

### Installation Manifests

Installation manifests are now attached as GitHub Release artifacts (as opposed to raw files checked into git)
and can be installed with the release download URL. e.g.:

```
kubectl apply -f https://github.com/argoproj/argo-rollouts/releases/download/v1.0.0/install.yaml
```

### Argo CD OutOfSync status on Rollout v1.0.0 CRDs:

Argo Rollouts v1.0 now vends apiextensions.k8s.io/v1 CustomResourceDefinitions (previously apiextensions.k8s.io/v1beta1).
Kubernetes v1 CRDs no longer supports the preservation of unknown fields in objects, and rejects
attempts to set `spec.preserveUnknownFields: true` (the previous default). In order to support a
smooth upgrade from Argo Rollouts v0.10 to v1.0, `spec.preserveUnknownFields` is explicitly set to
`false` in the manifests, despite `false` being the default, and only option in v1 CRDs. However 
this causes diffing tools (such as Argo CD) to report the manifest as OutOfSync (since K8s drops the
false field).

More information:
* https://github.com/kubernetes-sigs/controller-tools/issues/476
* https://github.com/argoproj/argo-rollouts/pull/1069

To avoid the Argo CD OutOfSync conditions, you can remove `spec.preserveUnknownFields` from the manifests
entirely *after upgrading to v1.0*.

Alternatively, you can instruct Argo CD to ignore differences using ignoreDifferences in the Application spec:

```yaml
spec:
  ignoreDifferences:
  - group: apiextensions.k8s.io
    kind: CustomResourceDefinition
    jsonPointers:
    - /spec/preserveUnknownFields
```

### Deprcation of `kubectl argo rollouts promote --skip-current-step` flag

The promote flag `--skip-current-step` which skips the current running canary step has been
deprecated and will be removed in a future release. Its logic to skipping the current step has
been merged with the existing command:

```shell
kubectl argo rollouts promote ROLLOUT
```

The `promote ROLLOUT` command can now be used to handle both the case where the rollout needs to be
unpaused, as well as to skip the currently running canary step (e.g. an analysis/experirment/pause
step).

## Changes since v0.10

### Controller
* feat: support reference model for workloads (#676) (#1072)
* feat: Implement Ambassador to be used as traffic router for canary deployments (#1025)
* feat: support canarying using Istio DestinationRule subsets (#985)
* feat: istio virtualservice and rollout in different namespaces
* feat: add ability to verify canary weights before advancing steps (#957)
* feat: support scaleDownDelaySeconds in canary w/ traffic routing (#1056)
* feat: Add ability to restart maxUnavailable pods to BlueGreen strategy (#937)
* feat(controller): Add support for ephemeral metadata on BlueGreen rollouts. Fixes #973 (#974)
* feat: Allow user to handle NaN result in Analysis (#977)
* feat: Wait for canary RS to have ready replicas before shifting labels (#1022)
* feat: Create RolloutPaused condition (#1054)
* feat: Add RolloutCompleted condition (#1074)
* feat: add print version flag to rollouts-controller
* feat: calculate rollout phase & message controller side
* fix: Fixes the regression of dropping resources from argo-rollouts crds. Fixes #1043 (#1044)
* fix: Set Canary Strategy default maxUnavailable to 25% (#981)
* fix: blue-green rollouts could pause prematurely during prePromotionAnalysis (#1007)
* fix: Clear ProgressDeadlineExceeded Condition in paused BlueGreen Rollout (#1002)
* fix: analysis template arguments validate (#1038)
* fix: calculate scale down count. (#1047)
* fix: verify analysis arguments name with those in the rollout (#1071)
* fix: rollout status always in progressing if analysis fails (#1099)
* fix: Add edge case handling to traffic routing (#1190)
* fix: unhandled error patchVirtualService (#1168)
* fix: handling error on f.close (#1167)
* fix: rollouts in middle of restart should be considered Progressing

### Analysis
* feat: metric fields can be parameterized by analysis arguments (#901)
* feat: support a custom base URL for the new relic provider (#1053)
* feat: Allow Datadog API and APP keys to be consumed from env vars (#1073)
* fix: Improve validation for AnalysisTemplates referenced by RO (#1094)
* fix: wavefront queries would return no datapoints. surface evaluate errors
* fix: metrics which errored did not retry at error interval
* fix: Improve and refactor validation for AnalysisTemplates

### Plugin
* feat: Argo Rollouts api-server and UI (#1015)
* feat: Implement rollout status command. Fixes #596 (#1001)
* feat: lint supporting rollout in multiple doc
* fix: get rollout always return not found except default namespace (#961)
* fix: create command not support namespace in yaml file (#962)
* fix: kubectl argo create panic: runtime error: invalid memory address or nil pointer dereference

### Misc
* chore: publish plugin image automatically. migrate to quay.io (#1102)
* feat: support ARM builds, remove unused components in Dockerfile (#889)
* chore: update k8s dependencies to v1.20. improve logging (#994) 
* fix: add informational exposed ports to deployment (#1066)
* chore: Outsource reusable UI components to argo-ux npm package
* fix: use fixed size int32

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
