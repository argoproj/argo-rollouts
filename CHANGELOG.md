
<a name="v1.6.6"></a>
## [v1.6.6](https://github.com/argoproj/argo-rollouts/compare/v1.6.5...v1.6.6) (2024-02-12)

### Fix

* stuck rollout when 2nd deployment happens before 1st finishes ([#3354](https://github.com/argoproj/argo-rollouts/issues/3354))
* do not require pod readiness when switching desired service selector on abort ([#3338](https://github.com/argoproj/argo-rollouts/issues/3338))


<a name="v1.6.5"></a>
## [v1.6.5](https://github.com/argoproj/argo-rollouts/compare/v1.6.4...v1.6.5) (2024-01-25)

### Chore

* add logging context around replicaset updates ([#3326](https://github.com/argoproj/argo-rollouts/issues/3326))
* remove year from codegen license  ([#3282](https://github.com/argoproj/argo-rollouts/issues/3282))

### Fix

* log rs name when update fails ([#3318](https://github.com/argoproj/argo-rollouts/issues/3318))
* keep rs inormer updated upon updating labels and annotations ([#3321](https://github.com/argoproj/argo-rollouts/issues/3321))
* updates to replicas and pod template at the same time causes rollout to get stuck ([#3272](https://github.com/argoproj/argo-rollouts/issues/3272))


<a name="v1.6.4"></a>
## [v1.6.4](https://github.com/argoproj/argo-rollouts/compare/v1.6.3...v1.6.4) (2023-12-08)

### Fix

* make sure we use the updated rs when we write back to informer ([#3237](https://github.com/argoproj/argo-rollouts/issues/3237))
* conflict on updates to replicaset revision ([#3216](https://github.com/argoproj/argo-rollouts/issues/3216))


<a name="v1.6.3"></a>
## [v1.6.3](https://github.com/argoproj/argo-rollouts/compare/v1.6.2...v1.6.3) (2023-12-04)

### Build

* **deps:** always resolve momentjs version 2.29.4 ([#3182](https://github.com/argoproj/argo-rollouts/issues/3182))

### Fix

* rollouts getting stuck due to bad rs informer updates ([#3200](https://github.com/argoproj/argo-rollouts/issues/3200))


<a name="v1.6.2"></a>
## [v1.6.2](https://github.com/argoproj/argo-rollouts/compare/v1.6.1...v1.6.2) (2023-11-02)

### Fix

* Revert "fix: istio destionationrule subsets enforcement ([#3126](https://github.com/argoproj/argo-rollouts/issues/3126))" ([#3147](https://github.com/argoproj/argo-rollouts/issues/3147))


<a name="v1.6.1"></a>
## [v1.6.1](https://github.com/argoproj/argo-rollouts/compare/v1.6.0...v1.6.1) (2023-11-01)

### Chore

* upgrade cosign ([#3139](https://github.com/argoproj/argo-rollouts/issues/3139))
* add missing rollout fields ([#3062](https://github.com/argoproj/argo-rollouts/issues/3062))
* change file name for readthedocs compatibility ([#2999](https://github.com/argoproj/argo-rollouts/issues/2999))

### Fix

* istio destionationrule subsets enforcement ([#3126](https://github.com/argoproj/argo-rollouts/issues/3126))
* docs require build.os to be defined ([#3133](https://github.com/argoproj/argo-rollouts/issues/3133))
* inopportune scaling events would lose some status fields ([#3060](https://github.com/argoproj/argo-rollouts/issues/3060))
* rollback to stable with dynamicStableScale could overwhelm stable pods ([#3077](https://github.com/argoproj/argo-rollouts/issues/3077))
* prevent hot loop when fully promoted rollout is aborted ([#3064](https://github.com/argoproj/argo-rollouts/issues/3064))
* keep rs informer updated ([#3091](https://github.com/argoproj/argo-rollouts/issues/3091))
* bump notification-engine to fix double send on self server notifications ([#3095](https://github.com/argoproj/argo-rollouts/issues/3095))
* sync notification controller configmaps/secrets first ([#3075](https://github.com/argoproj/argo-rollouts/issues/3075))
* missing notification on error ([#3076](https://github.com/argoproj/argo-rollouts/issues/3076))


<a name="v1.6.0"></a>
## [v1.6.0](https://github.com/argoproj/argo-rollouts/compare/v1.6.0-rc1...v1.6.0) (2023-09-05)

### Chore

* **deps:** bump github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 from 1.20.2 to 1.21.0 ([#2950](https://github.com/argoproj/argo-rollouts/issues/2950))
* **deps:** bump github.com/antonmedv/expr from 1.12.7 to 1.13.0 ([#2951](https://github.com/argoproj/argo-rollouts/issues/2951))

### Docs

* update supported k8s version ([#2949](https://github.com/argoproj/argo-rollouts/issues/2949))

### Fix

* analysis step should be ignored after promote ([#3016](https://github.com/argoproj/argo-rollouts/issues/3016))
* **controller:** rollback should skip all steps to active rs within RollbackWindow ([#2953](https://github.com/argoproj/argo-rollouts/issues/2953))
* **controller:** typo fix ("Secrete" -> "Secret") in secret informer ([#2965](https://github.com/argoproj/argo-rollouts/issues/2965))


<a name="v1.6.0-rc1"></a>
## [v1.6.0-rc1](https://github.com/argoproj/argo-rollouts/compare/v1.5.1...v1.6.0-rc1) (2023-08-10)

### Chore

* bump gotestsum and fix flakey test causing nil channel send ([#2934](https://github.com/argoproj/argo-rollouts/issues/2934))
* quote golang version string to not use go 1.2.2 ([#2915](https://github.com/argoproj/argo-rollouts/issues/2915))
* bump golang to 1.20 ([#2910](https://github.com/argoproj/argo-rollouts/issues/2910))
* add make help cmd ([#2854](https://github.com/argoproj/argo-rollouts/issues/2854))
* add unit test ([#2798](https://github.com/argoproj/argo-rollouts/issues/2798))
* Update test and related docs for plugin name standard ([#2728](https://github.com/argoproj/argo-rollouts/issues/2728))
* bump k8s deps to v0.25.8 ([#2712](https://github.com/argoproj/argo-rollouts/issues/2712))
* add zachaller as lead in owers file ([#2759](https://github.com/argoproj/argo-rollouts/issues/2759))
* Add tests for pause functionality in rollout package ([#2772](https://github.com/argoproj/argo-rollouts/issues/2772))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.26.0 to 1.26.1 ([#2840](https://github.com/argoproj/argo-rollouts/issues/2840))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.18.30 to 1.18.31 ([#2924](https://github.com/argoproj/argo-rollouts/issues/2924))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.27.0 to 1.27.1 ([#2927](https://github.com/argoproj/argo-rollouts/issues/2927))
* **deps:** bump docker/build-push-action from 4.0.0 to 4.1.0 ([#2832](https://github.com/argoproj/argo-rollouts/issues/2832))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.26.3 to 1.27.0 ([#2922](https://github.com/argoproj/argo-rollouts/issues/2922))
* **deps:** bump github.com/sirupsen/logrus from 1.9.2 to 1.9.3 ([#2821](https://github.com/argoproj/argo-rollouts/issues/2821))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.18.29 to 1.18.30 ([#2919](https://github.com/argoproj/argo-rollouts/issues/2919))
* **deps:** bump github.com/aws/aws-sdk-go-v2 from 1.19.0 to 1.19.1 ([#2920](https://github.com/argoproj/argo-rollouts/issues/2920))
* **deps:** bump google.golang.org/grpc from 1.56.2 to 1.57.0 ([#2908](https://github.com/argoproj/argo-rollouts/issues/2908))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.18.28 to 1.18.29 ([#2907](https://github.com/argoproj/argo-rollouts/issues/2907))
* **deps:** bump github.com/antonmedv/expr from 1.12.6 to 1.12.7 ([#2894](https://github.com/argoproj/argo-rollouts/issues/2894))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.26.2 to 1.26.3 ([#2884](https://github.com/argoproj/argo-rollouts/issues/2884))
* **deps:** bump docker/setup-qemu-action from 2.1.0 to 2.2.0 ([#2878](https://github.com/argoproj/argo-rollouts/issues/2878))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.18.27 to 1.18.28 ([#2883](https://github.com/argoproj/argo-rollouts/issues/2883))
* **deps:** bump slsa-framework/slsa-github-generator from 1.6.0 to 1.7.0 ([#2880](https://github.com/argoproj/argo-rollouts/issues/2880))
* **deps:** bump actions/setup-go from 4.0.0 to 4.0.1 ([#2881](https://github.com/argoproj/argo-rollouts/issues/2881))
* **deps:** bump docker/setup-buildx-action from 2.5.0 to 2.9.1 ([#2879](https://github.com/argoproj/argo-rollouts/issues/2879))
* **deps:** bump docker/login-action from 2.1.0 to 2.2.0 ([#2877](https://github.com/argoproj/argo-rollouts/issues/2877))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 from 1.19.13 to 1.19.14 ([#2886](https://github.com/argoproj/argo-rollouts/issues/2886))
* **deps:** bump github.com/antonmedv/expr from 1.12.5 to 1.12.6 ([#2882](https://github.com/argoproj/argo-rollouts/issues/2882))
* **deps:** bump google.golang.org/grpc from 1.56.1 to 1.56.2 ([#2872](https://github.com/argoproj/argo-rollouts/issues/2872))
* **deps:** bump sigstore/cosign-installer from 3.1.0 to 3.1.1 ([#2860](https://github.com/argoproj/argo-rollouts/issues/2860))
* **deps:** bump google.golang.org/protobuf from 1.30.0 to 1.31.0 ([#2859](https://github.com/argoproj/argo-rollouts/issues/2859))
* **deps:** bump sigstore/cosign-installer from 3.0.5 to 3.1.0 ([#2858](https://github.com/argoproj/argo-rollouts/issues/2858))
* **deps:** bump google.golang.org/grpc from 1.55.0 to 1.56.1 ([#2856](https://github.com/argoproj/argo-rollouts/issues/2856))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 from 1.19.12 to 1.19.13 ([#2847](https://github.com/argoproj/argo-rollouts/issues/2847))
* **deps:** bump actions/setup-go from 3.5.0 to 4.0.1 ([#2849](https://github.com/argoproj/argo-rollouts/issues/2849))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.18.26 to 1.18.27 ([#2844](https://github.com/argoproj/argo-rollouts/issues/2844))
* **deps:** bump github.com/prometheus/client_golang from 1.15.1 to 1.16.0 ([#2846](https://github.com/argoproj/argo-rollouts/issues/2846))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.26.1 to 1.26.2 ([#2848](https://github.com/argoproj/argo-rollouts/issues/2848))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 from 1.19.11 to 1.19.12 ([#2839](https://github.com/argoproj/argo-rollouts/issues/2839))
* **deps:** bump slsa-framework/slsa-github-generator from 1.7.0 to 1.8.0 ([#2936](https://github.com/argoproj/argo-rollouts/issues/2936))
* **deps:** bump docker/build-push-action from 4.1.0 to 4.1.1 ([#2837](https://github.com/argoproj/argo-rollouts/issues/2837))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.18.25 to 1.18.26 ([#2841](https://github.com/argoproj/argo-rollouts/issues/2841))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.18.31 to 1.18.32 ([#2928](https://github.com/argoproj/argo-rollouts/issues/2928))
* **deps:** bump github.com/hashicorp/go-plugin from 1.4.9 to 1.4.10 ([#2822](https://github.com/argoproj/argo-rollouts/issues/2822))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 from 1.19.14 to 1.20.1 ([#2926](https://github.com/argoproj/argo-rollouts/issues/2926))
* **deps:** bump github.com/stretchr/testify from 1.8.3 to 1.8.4 ([#2817](https://github.com/argoproj/argo-rollouts/issues/2817))
* **deps:** bump github.com/sirupsen/logrus from 1.9.1 to 1.9.2 ([#2789](https://github.com/argoproj/argo-rollouts/issues/2789))
* **deps:** bump github.com/stretchr/testify from 1.8.2 to 1.8.3 ([#2796](https://github.com/argoproj/argo-rollouts/issues/2796))
* **deps:** bump sigstore/cosign-installer from 3.0.3 to 3.0.5 ([#2788](https://github.com/argoproj/argo-rollouts/issues/2788))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 from 1.20.1 to 1.20.2 ([#2941](https://github.com/argoproj/argo-rollouts/issues/2941))
* **deps:** bump github.com/sirupsen/logrus from 1.9.0 to 1.9.1 ([#2784](https://github.com/argoproj/argo-rollouts/issues/2784))
* **deps:** bump codecov/codecov-action from 3.1.3 to 3.1.4 ([#2782](https://github.com/argoproj/argo-rollouts/issues/2782))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.18.24 to 1.18.25 ([#2770](https://github.com/argoproj/argo-rollouts/issues/2770))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.18.23 to 1.18.24 ([#2768](https://github.com/argoproj/argo-rollouts/issues/2768))
* **deps:** bump google.golang.org/grpc from 1.54.0 to 1.55.0 ([#2763](https://github.com/argoproj/argo-rollouts/issues/2763))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.18.22 to 1.18.23 ([#2756](https://github.com/argoproj/argo-rollouts/issues/2756))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.27.1 to 1.27.2 ([#2944](https://github.com/argoproj/argo-rollouts/issues/2944))
* **deps:** replace `github.com/ghodss/yaml` with `sigs.k8s.io/yaml` ([#2681](https://github.com/argoproj/argo-rollouts/issues/2681))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.25.10 to 1.26.0 ([#2755](https://github.com/argoproj/argo-rollouts/issues/2755))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 from 1.19.10 to 1.19.11 ([#2757](https://github.com/argoproj/argo-rollouts/issues/2757))
* **deps:** bump github.com/prometheus/client_golang from 1.15.0 to 1.15.1 ([#2754](https://github.com/argoproj/argo-rollouts/issues/2754))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.18.21 to 1.18.22 ([#2746](https://github.com/argoproj/argo-rollouts/issues/2746))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.25.9 to 1.25.10 ([#2745](https://github.com/argoproj/argo-rollouts/issues/2745))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.18.32 to 1.18.33 ([#2943](https://github.com/argoproj/argo-rollouts/issues/2943))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 from 1.19.9 to 1.19.10 ([#2747](https://github.com/argoproj/argo-rollouts/issues/2747))
* **deps:** bump codecov/codecov-action from 3.1.2 to 3.1.3 ([#2735](https://github.com/argoproj/argo-rollouts/issues/2735))
* **deps:** bump actions/setup-go from 4.0.1 to 4.1.0 ([#2947](https://github.com/argoproj/argo-rollouts/issues/2947))
* **deps:** bump github.com/prometheus/client_golang from 1.14.0 to 1.15.0 ([#2721](https://github.com/argoproj/argo-rollouts/issues/2721))
* **deps:** bump codecov/codecov-action from 3.1.1 to 3.1.2 ([#2711](https://github.com/argoproj/argo-rollouts/issues/2711))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.18.20 to 1.18.21 ([#2709](https://github.com/argoproj/argo-rollouts/issues/2709))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 from 1.19.8 to 1.19.9 ([#2708](https://github.com/argoproj/argo-rollouts/issues/2708))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.25.8 to 1.25.9 ([#2710](https://github.com/argoproj/argo-rollouts/issues/2710))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.18.19 to 1.18.20 ([#2705](https://github.com/argoproj/argo-rollouts/issues/2705))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 from 1.19.7 to 1.19.8 ([#2704](https://github.com/argoproj/argo-rollouts/issues/2704))
* **deps:** bump github.com/aws/aws-sdk-go-v2 from 1.17.7 to 1.17.8 ([#2703](https://github.com/argoproj/argo-rollouts/issues/2703))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.25.7 to 1.25.8 ([#2702](https://github.com/argoproj/argo-rollouts/issues/2702))
* **deps:** bump peter-evans/create-pull-request from 4 to 5 ([#2697](https://github.com/argoproj/argo-rollouts/issues/2697))
* **deps:** bump github.com/spf13/cobra from 1.6.1 to 1.7.0 ([#2698](https://github.com/argoproj/argo-rollouts/issues/2698))
* **deps:** bump github.com/influxdata/influxdb-client-go/v2 from 2.12.2 to 2.12.3 ([#2684](https://github.com/argoproj/argo-rollouts/issues/2684))

### Ci

* generate attestations during a release  ([#2785](https://github.com/argoproj/argo-rollouts/issues/2785))
* use keyless signing for main and release branches ([#2783](https://github.com/argoproj/argo-rollouts/issues/2783))

### Docs

* support for Google Cloud Load balancers ([#2803](https://github.com/argoproj/argo-rollouts/issues/2803))
* Update Changelog ([#2683](https://github.com/argoproj/argo-rollouts/issues/2683))
* mirroring support in Traefik is not implemented yet ([#2904](https://github.com/argoproj/argo-rollouts/issues/2904))
* Update docs of Rollout spec to add active/previewMetadata ([#2833](https://github.com/argoproj/argo-rollouts/issues/2833))
* Update datadog.md - clarify formulas [#2813](https://github.com/argoproj/argo-rollouts/issues/2813) ([#2819](https://github.com/argoproj/argo-rollouts/issues/2819))
* support for Kong ingress ([#2820](https://github.com/argoproj/argo-rollouts/issues/2820))
* Fix AWS App Mesh getting started documentation to avoid connection pooling problems ([#2814](https://github.com/argoproj/argo-rollouts/issues/2814))
* Update Changelog ([#2807](https://github.com/argoproj/argo-rollouts/issues/2807))
* use correct capitalization for "Datadog" in navigation sidebar ([#2809](https://github.com/argoproj/argo-rollouts/issues/2809))
* Add gateway API link, fix Contour plugin naming ([#2787](https://github.com/argoproj/argo-rollouts/issues/2787))
* fix minor mistakes in Migrating to Deployments ([#2270](https://github.com/argoproj/argo-rollouts/issues/2270))
* Show how plugins are loaded ([#2801](https://github.com/argoproj/argo-rollouts/issues/2801))
* Fix typo in header routing specification docs ([#2808](https://github.com/argoproj/argo-rollouts/issues/2808))
* Add some details around running locally to make things clearer new contributors ([#2786](https://github.com/argoproj/argo-rollouts/issues/2786))
* Add docs for Amazon Managed Prometheus ([#2777](https://github.com/argoproj/argo-rollouts/issues/2777))
* Update Changelog ([#2765](https://github.com/argoproj/argo-rollouts/issues/2765))
* copy argo cd docs drop down fix ([#2731](https://github.com/argoproj/argo-rollouts/issues/2731))
* Add contour trafficrouter plugin ([#2729](https://github.com/argoproj/argo-rollouts/issues/2729))
* fix link to plugins for traffic routers ([#2719](https://github.com/argoproj/argo-rollouts/issues/2719))
* update contributions.md to include k3d as recommended cluster, add details on e2e test setup, and update kubectl install link. Fixes [#1750](https://github.com/argoproj/argo-rollouts/issues/1750) ([#1867](https://github.com/argoproj/argo-rollouts/issues/1867))
* **analysis:** fix use stringData in the examples ([#2715](https://github.com/argoproj/argo-rollouts/issues/2715))
* **example:** interval requires count ([#2690](https://github.com/argoproj/argo-rollouts/issues/2690))
* **example:** Add example on how to execute subset of e2e tests ([#2867](https://github.com/argoproj/argo-rollouts/issues/2867))

### Feat

* enable self service notification support ([#2930](https://github.com/argoproj/argo-rollouts/issues/2930))
* support prometheus headers ([#2937](https://github.com/argoproj/argo-rollouts/issues/2937))
* Add insecure option for Prometheus. Fixes [#2913](https://github.com/argoproj/argo-rollouts/issues/2913) ([#2914](https://github.com/argoproj/argo-rollouts/issues/2914))
* Add prometheus timeout ([#2893](https://github.com/argoproj/argo-rollouts/issues/2893))
* Support Multiple ALB Ingresses ([#2639](https://github.com/argoproj/argo-rollouts/issues/2639))
* Send informer add k8s event ([#2834](https://github.com/argoproj/argo-rollouts/issues/2834))
* add merge key to analysis template ([#2842](https://github.com/argoproj/argo-rollouts/issues/2842))
* retain TLS configuration for canary ingresses in the nginx integration. Fixes [#1134](https://github.com/argoproj/argo-rollouts/issues/1134) ([#2679](https://github.com/argoproj/argo-rollouts/issues/2679))
* **analysis:** Adds rollout Spec.Selector.MatchLabels to AnalysisRun. Fixes [#2888](https://github.com/argoproj/argo-rollouts/issues/2888) ([#2903](https://github.com/argoproj/argo-rollouts/issues/2903))
* **controller:** Add custom metadata support for AnalysisRun. Fixes [#2740](https://github.com/argoproj/argo-rollouts/issues/2740) ([#2743](https://github.com/argoproj/argo-rollouts/issues/2743))
* **dashboard:** Refresh Rollouts dashboard UI ([#2723](https://github.com/argoproj/argo-rollouts/issues/2723))
* **metricprovider:** allow user to define metrics.provider.job.metadata ([#2762](https://github.com/argoproj/argo-rollouts/issues/2762))

### Fix

* istio dropping fields during removing of managed routes ([#2692](https://github.com/argoproj/argo-rollouts/issues/2692))
* resolve args to metric in garbage collection function ([#2843](https://github.com/argoproj/argo-rollouts/issues/2843))
*  rollout not modify the VirtualService whit setHeaderRoute step with workloadRef ([#2797](https://github.com/argoproj/argo-rollouts/issues/2797))
* get new httpRoutesI after removeRoute() to avoid duplicates. Fixes [#2769](https://github.com/argoproj/argo-rollouts/issues/2769) ([#2887](https://github.com/argoproj/argo-rollouts/issues/2887))
* make new alb fullName field  optional for backward compatability ([#2806](https://github.com/argoproj/argo-rollouts/issues/2806))
* change logic of analysis run to better handle errors ([#2695](https://github.com/argoproj/argo-rollouts/issues/2695))
* cloudwatch metrics provider multiple dimensions ([#2932](https://github.com/argoproj/argo-rollouts/issues/2932))
* add required ingress permission ([#2933](https://github.com/argoproj/argo-rollouts/issues/2933))
* properly wrap Datadog API v2 request body ([#2771](https://github.com/argoproj/argo-rollouts/issues/2771)) ([#2775](https://github.com/argoproj/argo-rollouts/issues/2775))
* **analysis:** Graphite query - remove whitespaces ([#2752](https://github.com/argoproj/argo-rollouts/issues/2752))
* **analysis:** Graphite metric provider - index out of range [0] with length 0 ([#2751](https://github.com/argoproj/argo-rollouts/issues/2751))
* **analysis:** Adding field in YAML to provide region for Sigv4 signing.  ([#2794](https://github.com/argoproj/argo-rollouts/issues/2794))
* **controller:** Fix for rollouts getting stuck in loop ([#2689](https://github.com/argoproj/argo-rollouts/issues/2689))
* **controller:** Remove name label from some k8s client metrics on events and replicasets ([#2851](https://github.com/argoproj/argo-rollouts/issues/2851))
* **controller:** Add klog logrus bridge. Fixes [#2707](https://github.com/argoproj/argo-rollouts/issues/2707). ([#2701](https://github.com/argoproj/argo-rollouts/issues/2701))
* **trafficrouting:** apply stable selectors on canary service on rollout abort [#2781](https://github.com/argoproj/argo-rollouts/issues/2781) ([#2818](https://github.com/argoproj/argo-rollouts/issues/2818))

### Refactor

* change plugin naming pattern [#2720](https://github.com/argoproj/argo-rollouts/issues/2720) ([#2722](https://github.com/argoproj/argo-rollouts/issues/2722))

### BREAKING CHANGE


The metric labels have changed on controller_clientset_k8s_request_total to not include the name of the resource for events and replicasets. These names have generated hashes in them and cause really high cardinality.

Remove name label from k8s some client metrics

The `name` label in the `controller_clientset_k8s_request_total` metric
produce an excessive amount of cardinality for `events` and `replicasets`.
This can lead to hundreds of thousands of unique metrics over a couple
weeks in a large deployment. Set the name to "N/A" for these client request
types.


<a name="v1.5.1"></a>
## [v1.5.1](https://github.com/argoproj/argo-rollouts/compare/v1.5.0...v1.5.1) (2023-05-24)

### Ci

* use keyless signing for main and release branches ([#2783](https://github.com/argoproj/argo-rollouts/issues/2783))

### Fix

* make new alb fullName field  optional for backward compatability ([#2806](https://github.com/argoproj/argo-rollouts/issues/2806))
* properly wrap Datadog API v2 request body ([#2771](https://github.com/argoproj/argo-rollouts/issues/2771)) ([#2775](https://github.com/argoproj/argo-rollouts/issues/2775))


<a name="v1.5.0"></a>
## [v1.5.0](https://github.com/argoproj/argo-rollouts/compare/v1.5.0-rc1...v1.5.0) (2023-05-05)

### Chore

* bump k8s deps to v0.25.8 ([#2712](https://github.com/argoproj/argo-rollouts/issues/2712))

### Docs

* fix link to plugins for traffic routers ([#2719](https://github.com/argoproj/argo-rollouts/issues/2719))
* copy argo cd docs drop down fix ([#2731](https://github.com/argoproj/argo-rollouts/issues/2731))

### Fix

* istio dropping fields during removing of managed routes ([#2692](https://github.com/argoproj/argo-rollouts/issues/2692))
* change logic of analysis run to better handle errors ([#2695](https://github.com/argoproj/argo-rollouts/issues/2695))
* **controller:** Fix for rollouts getting stuck in loop ([#2689](https://github.com/argoproj/argo-rollouts/issues/2689))
* **controller:** Add klog logrus bridge. Fixes [#2707](https://github.com/argoproj/argo-rollouts/issues/2707). ([#2701](https://github.com/argoproj/argo-rollouts/issues/2701))


<a name="v1.5.0-rc1"></a>
## [v1.5.0-rc1](https://github.com/argoproj/argo-rollouts/compare/v1.4.1...v1.5.0-rc1) (2023-03-27)

### Build

* manually run auto changelog and fix workflow ([#2494](https://github.com/argoproj/argo-rollouts/issues/2494))

### Chore

* update e2e k8s versions ([#2637](https://github.com/argoproj/argo-rollouts/issues/2637))
* Remove namespaced crds ([#2516](https://github.com/argoproj/argo-rollouts/issues/2516))
* fix dependabot broken dependency ([#2529](https://github.com/argoproj/argo-rollouts/issues/2529))
* disable docker sbom and attestations ([#2528](https://github.com/argoproj/argo-rollouts/issues/2528))
* improve e2e test timing ([#2577](https://github.com/argoproj/argo-rollouts/issues/2577))
* fix typo for json tag on rollbackWindow ([#2598](https://github.com/argoproj/argo-rollouts/issues/2598))
* update package dependencie ([#2602](https://github.com/argoproj/argo-rollouts/issues/2602))
* bump node version and set openssl-legacy-provider ([#2606](https://github.com/argoproj/argo-rollouts/issues/2606))
* switch to distroless for cli/dashboard image ([#2596](https://github.com/argoproj/argo-rollouts/issues/2596))
* add Tuhu to users ([#2630](https://github.com/argoproj/argo-rollouts/issues/2630))
* bump deps for prisma ([#2643](https://github.com/argoproj/argo-rollouts/issues/2643))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.18.13 to 1.18.14 ([#2614](https://github.com/argoproj/argo-rollouts/issues/2614))
* **deps:** bump github.com/antonmedv/expr from 1.12.3 to 1.12.5 ([#2670](https://github.com/argoproj/argo-rollouts/issues/2670))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.18.16 to 1.18.17 ([#2659](https://github.com/argoproj/argo-rollouts/issues/2659))
* **deps:** bump github.com/antonmedv/expr from 1.12.2 to 1.12.3 ([#2653](https://github.com/argoproj/argo-rollouts/issues/2653))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 from 1.19.5 to 1.19.6 ([#2654](https://github.com/argoproj/argo-rollouts/issues/2654))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.25.4 to 1.25.5 ([#2655](https://github.com/argoproj/argo-rollouts/issues/2655))
* **deps:** bump github.com/antonmedv/expr from 1.12.1 to 1.12.2 ([#2649](https://github.com/argoproj/argo-rollouts/issues/2649))
* **deps:** bump google.golang.org/protobuf from 1.28.1 to 1.29.0 ([#2646](https://github.com/argoproj/argo-rollouts/issues/2646))
* **deps:** bump github.com/golang/protobuf from 1.5.2 to 1.5.3 ([#2645](https://github.com/argoproj/argo-rollouts/issues/2645))
* **deps:** bump github.com/prometheus/common from 0.41.0 to 0.42.0 ([#2644](https://github.com/argoproj/argo-rollouts/issues/2644))
* **deps:** bump minimist from 1.2.5 to 1.2.8 in /ui ([#2638](https://github.com/argoproj/argo-rollouts/issues/2638))
* **deps:** bump github.com/hashicorp/go-plugin from 1.4.8 to 1.4.9 ([#2636](https://github.com/argoproj/argo-rollouts/issues/2636))
* **deps:** bump github.com/prometheus/common from 0.40.0 to 0.41.0 ([#2634](https://github.com/argoproj/argo-rollouts/issues/2634))
* **deps:** bump google.golang.org/protobuf from 1.29.0 to 1.29.1 ([#2660](https://github.com/argoproj/argo-rollouts/issues/2660))
* **deps:** bump google.golang.org/protobuf from 1.29.1 to 1.30.0 ([#2665](https://github.com/argoproj/argo-rollouts/issues/2665))
* **deps:** bump github.com/stretchr/testify from 1.8.1 to 1.8.2 ([#2627](https://github.com/argoproj/argo-rollouts/issues/2627))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.18.14 to 1.18.15 ([#2618](https://github.com/argoproj/argo-rollouts/issues/2618))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.25.3 to 1.25.4 ([#2617](https://github.com/argoproj/argo-rollouts/issues/2617))
* **deps:** bump github.com/antonmedv/expr from 1.12.0 to 1.12.1 ([#2619](https://github.com/argoproj/argo-rollouts/issues/2619))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 from 1.19.4 to 1.19.5 ([#2616](https://github.com/argoproj/argo-rollouts/issues/2616))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 from 1.19.3 to 1.19.4 ([#2612](https://github.com/argoproj/argo-rollouts/issues/2612))
* **deps:** bump github.com/prometheus/common from 0.39.0 to 0.40.0 ([#2611](https://github.com/argoproj/argo-rollouts/issues/2611))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.25.6 to 1.25.7 ([#2682](https://github.com/argoproj/argo-rollouts/issues/2682))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.25.2 to 1.25.3 ([#2615](https://github.com/argoproj/argo-rollouts/issues/2615))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 from 1.19.6 to 1.19.7 ([#2672](https://github.com/argoproj/argo-rollouts/issues/2672))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.18.17 to 1.18.19 ([#2673](https://github.com/argoproj/argo-rollouts/issues/2673))
* **deps:** bump imjasonh/setup-crane from 0.2 to 0.3 ([#2600](https://github.com/argoproj/argo-rollouts/issues/2600))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.25.5 to 1.25.6 ([#2671](https://github.com/argoproj/argo-rollouts/issues/2671))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config ([#2593](https://github.com/argoproj/argo-rollouts/issues/2593))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.18.15 to 1.18.16 ([#2652](https://github.com/argoproj/argo-rollouts/issues/2652))
* **deps:** bump google.golang.org/grpc from 1.52.3 to 1.53.0 ([#2574](https://github.com/argoproj/argo-rollouts/issues/2574))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 ([#2565](https://github.com/argoproj/argo-rollouts/issues/2565))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config ([#2564](https://github.com/argoproj/argo-rollouts/issues/2564))
* **deps:** bump github.com/antonmedv/expr from 1.11.0 to 1.12.0 ([#2567](https://github.com/argoproj/argo-rollouts/issues/2567))
* **deps:** bump github.com/aws/aws-sdk-go-v2 from 1.17.3 to 1.17.4 ([#2566](https://github.com/argoproj/argo-rollouts/issues/2566))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch ([#2563](https://github.com/argoproj/argo-rollouts/issues/2563))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 ([#2559](https://github.com/argoproj/argo-rollouts/issues/2559))
* **deps:** bump github.com/antonmedv/expr from 1.9.0 to 1.11.0 ([#2558](https://github.com/argoproj/argo-rollouts/issues/2558))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config ([#2555](https://github.com/argoproj/argo-rollouts/issues/2555))
* **deps:** bump docker/build-push-action from 3.3.0 to 4.0.0 ([#2550](https://github.com/argoproj/argo-rollouts/issues/2550))
* **deps:** bump github.com/influxdata/influxdb-client-go/v2 ([#2544](https://github.com/argoproj/argo-rollouts/issues/2544))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config ([#2542](https://github.com/argoproj/argo-rollouts/issues/2542))
* **deps:** bump google.golang.org/grpc from 1.52.1 to 1.52.3 ([#2541](https://github.com/argoproj/argo-rollouts/issues/2541))
* **deps:** bump google.golang.org/grpc from 1.52.0 to 1.52.1 ([#2538](https://github.com/argoproj/argo-rollouts/issues/2538))
* **deps:** bump dependabot/fetch-metadata from 1.3.5 to 1.3.6 ([#2537](https://github.com/argoproj/argo-rollouts/issues/2537))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch ([#2534](https://github.com/argoproj/argo-rollouts/issues/2534))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config ([#2533](https://github.com/argoproj/argo-rollouts/issues/2533))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 ([#2532](https://github.com/argoproj/argo-rollouts/issues/2532))
* **deps:** bump google.golang.org/grpc from 1.53.0 to 1.54.0 ([#2674](https://github.com/argoproj/argo-rollouts/issues/2674))
* **deps:** bump actions/setup-go from 3 to 4 ([#2663](https://github.com/argoproj/argo-rollouts/issues/2663))
* **deps:** bump github.com/antonmedv/expr from 1.9.0 to 1.10.0 ([#2527](https://github.com/argoproj/argo-rollouts/issues/2527))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch ([#2523](https://github.com/argoproj/argo-rollouts/issues/2523))
* **deps:** bump actions/stale from 7 to 8 ([#2677](https://github.com/argoproj/argo-rollouts/issues/2677))
* **deps:** bump google.golang.org/grpc from 1.51.0 to 1.52.0 ([#2513](https://github.com/argoproj/argo-rollouts/issues/2513))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch ([#2505](https://github.com/argoproj/argo-rollouts/issues/2505))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 ([#2506](https://github.com/argoproj/argo-rollouts/issues/2506))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config ([#2504](https://github.com/argoproj/argo-rollouts/issues/2504))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config ([#2497](https://github.com/argoproj/argo-rollouts/issues/2497))
* **deps:** bump actions/stale from 6 to 7 ([#2496](https://github.com/argoproj/argo-rollouts/issues/2496))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config ([#2492](https://github.com/argoproj/argo-rollouts/issues/2492))

### Docs

* Mention Internet Bug Bounty in the security policy ([#2642](https://github.com/argoproj/argo-rollouts/issues/2642))
* Update Changelog ([#2625](https://github.com/argoproj/argo-rollouts/issues/2625))
* fix missing links for getting started documentation ([#2557](https://github.com/argoproj/argo-rollouts/issues/2557))
* fix spelling in example notification templates ([#2554](https://github.com/argoproj/argo-rollouts/issues/2554))
* Add best practice for reducing memory usage ([#2545](https://github.com/argoproj/argo-rollouts/issues/2545))
* commit generated docs for readthedocs.org ([#2535](https://github.com/argoproj/argo-rollouts/issues/2535))
* fix incorrect description for autoPromotionSeconds ([#2525](https://github.com/argoproj/argo-rollouts/issues/2525))
* manually add changelog due to action failure ([#2510](https://github.com/argoproj/argo-rollouts/issues/2510))
* fix typo apisix ([#2508](https://github.com/argoproj/argo-rollouts/issues/2508))
* add release schedule ([#2446](https://github.com/argoproj/argo-rollouts/issues/2446))
* fix rendering by upgrading deps ([#2495](https://github.com/argoproj/argo-rollouts/issues/2495))

### Feat

* Apache APISIX SetHeader support. Fixes [#2668](https://github.com/argoproj/argo-rollouts/issues/2668) ([#2678](https://github.com/argoproj/argo-rollouts/issues/2678))
* support N nginx ingresses ([#2467](https://github.com/argoproj/argo-rollouts/issues/2467))
* Add Service field to Rollout Experiment to allow service creation ([#2633](https://github.com/argoproj/argo-rollouts/issues/2633))
* Provide time.Parse and time.Now while evaluating notification trigger condition ([#2206](https://github.com/argoproj/argo-rollouts/issues/2206))
* Allow switching between Datadog v1 and v2. Fixes [#2549](https://github.com/argoproj/argo-rollouts/issues/2549) ([#2592](https://github.com/argoproj/argo-rollouts/issues/2592))
* add support for traffic router plugins ([#2573](https://github.com/argoproj/argo-rollouts/issues/2573))
* Add name attribute to ServicePort ([#2572](https://github.com/argoproj/argo-rollouts/issues/2572))
* metric plugin system based on hashicorp go-plugin ([#2514](https://github.com/argoproj/argo-rollouts/issues/2514))
* Adding SigV4 Option for Prometheus Metric Analysis ([#2489](https://github.com/argoproj/argo-rollouts/issues/2489))
* **analysis:** add Apache SkyWalking as metrics provider
* **controller:** Adding status.alb.canaryTargetGroup.fullName for ALB. Fixes [#2589](https://github.com/argoproj/argo-rollouts/issues/2589) ([#2604](https://github.com/argoproj/argo-rollouts/issues/2604))

### Fix

* switch service selector back to stable on canary service when aborted ([#2540](https://github.com/argoproj/argo-rollouts/issues/2540))
* change log generator to only add CHANGELOG.md ([#2626](https://github.com/argoproj/argo-rollouts/issues/2626))
* Rollback change on service creation with weightless experiments ([#2624](https://github.com/argoproj/argo-rollouts/issues/2624))
* flakey TestWriteBackToInformer test ([#2621](https://github.com/argoproj/argo-rollouts/issues/2621))
* remove outdated ioutil package dependencies ([#2583](https://github.com/argoproj/argo-rollouts/issues/2583))
* update GetTargetGroupMetadata to call DescribeTags in batches ([#2570](https://github.com/argoproj/argo-rollouts/issues/2570))
* analysis information box [#2530](https://github.com/argoproj/argo-rollouts/issues/2530)  ([#2575](https://github.com/argoproj/argo-rollouts/issues/2575))
* support only tls in virtual services ([#2502](https://github.com/argoproj/argo-rollouts/issues/2502))
* **analysis:** Nil Pointer Fixes [#2458](https://github.com/argoproj/argo-rollouts/issues/2458) ([#2680](https://github.com/argoproj/argo-rollouts/issues/2680))

### BREAKING CHANGE


There was an unintentional change in behavior related to service creation with experiments introduced in v1.4.0 this has been reverted in v1.4.1 back to the original behavior. In v1.4.0 services where always created with for inline experiments even if there was no weight set. In 1.4.1 we go back to the original behavior of requiring weight to be set in order to create a service.


<a name="v1.4.1"></a>
## [v1.4.1](https://github.com/argoproj/argo-rollouts/compare/v1.4.0...v1.4.1) (2023-02-20)

### Build

* manually run auto changelog and fix workflow ([#2494](https://github.com/argoproj/argo-rollouts/issues/2494))

### Chore

* bump node version and set openssl-legacy-provider ([#2606](https://github.com/argoproj/argo-rollouts/issues/2606))
* fix typo for json tag on rollbackWindow ([#2598](https://github.com/argoproj/argo-rollouts/issues/2598))
* disable docker sbom and attestations ([#2528](https://github.com/argoproj/argo-rollouts/issues/2528))

### Docs

* commit generated docs for readthedocs.org ([#2535](https://github.com/argoproj/argo-rollouts/issues/2535))

### Feat

* Add name attribute to ServicePort ([#2572](https://github.com/argoproj/argo-rollouts/issues/2572))

### Fix

* update GetTargetGroupMetadata to call DescribeTags in batches ([#2570](https://github.com/argoproj/argo-rollouts/issues/2570))
* Rollback change on service creation with weightless experiments ([#2624](https://github.com/argoproj/argo-rollouts/issues/2624))

### BREAKING CHANGE


There was an unintentional change in behavior related to service creation with experiments introduced in v1.4.0 this has been reverted in v1.4.1 back to the original behavior. In v1.4.0 services where always created with for inline experiments even if there was no weight set. In 1.4.1 we go back to the original behavior of requiring weight to be set in order to create a service.


<a name="v1.4.0"></a>
## [v1.4.0](https://github.com/argoproj/argo-rollouts/compare/v1.4.0-rc1...v1.4.0) (2023-01-03)

### Docs

* fix rendering by upgrading deps ([#2495](https://github.com/argoproj/argo-rollouts/issues/2495))

### Fix

* support only tls in virtual services ([#2502](https://github.com/argoproj/argo-rollouts/issues/2502))


<a name="v1.4.0-rc1"></a>
## [v1.4.0-rc1](https://github.com/argoproj/argo-rollouts/compare/v1.3.3...v1.4.0-rc1) (2022-12-20)

### Build

* use fixed docker repository because we can't reach accross jobs ([#2474](https://github.com/argoproj/argo-rollouts/issues/2474))
* copy proto files from GOPATH so we can clone outside of GOPATH ([#2360](https://github.com/argoproj/argo-rollouts/issues/2360))
* add sha256 checksums for all released bins ([#2332](https://github.com/argoproj/argo-rollouts/issues/2332))

### Chore

* add optum to users list ([#2466](https://github.com/argoproj/argo-rollouts/issues/2466))
* upgrade golang to 1.19 ([#2219](https://github.com/argoproj/argo-rollouts/issues/2219))
* sign container images and checksum assets ([#2334](https://github.com/argoproj/argo-rollouts/issues/2334))
* update stable tag conditionally ([#2480](https://github.com/argoproj/argo-rollouts/issues/2480))
* fix checksum generation ([#2481](https://github.com/argoproj/argo-rollouts/issues/2481))
* Add Yotpo to USERS.md
* use docker login to sign images ([#2479](https://github.com/argoproj/argo-rollouts/issues/2479))
* use correct image for plugin container ([#2478](https://github.com/argoproj/argo-rollouts/issues/2478))
* rename the examples/trafffic-management directory to istio ([#2315](https://github.com/argoproj/argo-rollouts/issues/2315))
* Add example for istio-subset-split ([#2318](https://github.com/argoproj/argo-rollouts/issues/2318))
* add deprecation notice for rollout_phase in docs ([#2377](https://github.com/argoproj/argo-rollouts/issues/2377)) ([#2378](https://github.com/argoproj/argo-rollouts/issues/2378))
* remove deprecated -i for go build ([#2047](https://github.com/argoproj/argo-rollouts/issues/2047))
* **cli:** add darwin arm64 to build and release ([#2264](https://github.com/argoproj/argo-rollouts/issues/2264))
* **deps:** upgrade ui deps to fix high security cve's ([#2345](https://github.com/argoproj/argo-rollouts/issues/2345))
* **deps:** bump github.com/aws/aws-sdk-go-v2 from 1.17.0 to 1.17.1 ([#2369](https://github.com/argoproj/argo-rollouts/issues/2369))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch ([#2455](https://github.com/argoproj/argo-rollouts/issues/2455))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 ([#2454](https://github.com/argoproj/argo-rollouts/issues/2454))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config ([#2452](https://github.com/argoproj/argo-rollouts/issues/2452))
* **deps:** bump github.com/influxdata/influxdb-client-go/v2 ([#2447](https://github.com/argoproj/argo-rollouts/issues/2447))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch ([#2439](https://github.com/argoproj/argo-rollouts/issues/2439))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch ([#2430](https://github.com/argoproj/argo-rollouts/issues/2430))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config ([#2429](https://github.com/argoproj/argo-rollouts/issues/2429))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 ([#2428](https://github.com/argoproj/argo-rollouts/issues/2428))
* **deps:** bump google.golang.org/grpc from 1.50.1 to 1.51.0 ([#2421](https://github.com/argoproj/argo-rollouts/issues/2421))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config ([#2418](https://github.com/argoproj/argo-rollouts/issues/2418))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 ([#2417](https://github.com/argoproj/argo-rollouts/issues/2417))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch ([#2414](https://github.com/argoproj/argo-rollouts/issues/2414))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config ([#2413](https://github.com/argoproj/argo-rollouts/issues/2413))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 ([#2412](https://github.com/argoproj/argo-rollouts/issues/2412))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config ([#2409](https://github.com/argoproj/argo-rollouts/issues/2409))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 ([#2406](https://github.com/argoproj/argo-rollouts/issues/2406))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch ([#2404](https://github.com/argoproj/argo-rollouts/issues/2404))
* **deps:** bump github.com/prometheus/client_golang ([#2469](https://github.com/argoproj/argo-rollouts/issues/2469))
* **deps:** bump codecov/codecov-action from 2.1.0 to 3.1.1 ([#2251](https://github.com/argoproj/argo-rollouts/issues/2251))
* **deps:** bump notification engine ([#2470](https://github.com/argoproj/argo-rollouts/issues/2470))
* **deps:** bump github.com/prometheus/common from 0.38.0 to 0.39.0 ([#2476](https://github.com/argoproj/argo-rollouts/issues/2476))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch ([#2477](https://github.com/argoproj/argo-rollouts/issues/2477))
* **deps:** bump dependabot/fetch-metadata from 1.3.4 to 1.3.5 ([#2390](https://github.com/argoproj/argo-rollouts/issues/2390))
* **deps:** bump imjasonh/setup-crane from 0.1 to 0.2 ([#2387](https://github.com/argoproj/argo-rollouts/issues/2387))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch ([#2487](https://github.com/argoproj/argo-rollouts/issues/2487))
* **deps:** bump github.com/aws/aws-sdk-go-v2 from 1.17.2 to 1.17.3 ([#2484](https://github.com/argoproj/argo-rollouts/issues/2484))
* **deps:** bump actions/upload-artifact from 2 to 3 ([#1973](https://github.com/argoproj/argo-rollouts/issues/1973))
* **deps:** bump github.com/influxdata/influxdb-client-go/v2 ([#2381](https://github.com/argoproj/argo-rollouts/issues/2381))
* **deps:** bump github.com/spf13/cobra from 1.6.0 to 1.6.1 ([#2370](https://github.com/argoproj/argo-rollouts/issues/2370))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch ([#2366](https://github.com/argoproj/argo-rollouts/issues/2366))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config ([#2367](https://github.com/argoproj/argo-rollouts/issues/2367))
* **deps:** bump github.com/prometheus/common from 0.37.0 to 0.38.0 ([#2468](https://github.com/argoproj/argo-rollouts/issues/2468))
* **deps:** bump github.com/stretchr/testify from 1.8.0 to 1.8.1 ([#2368](https://github.com/argoproj/argo-rollouts/issues/2368))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 ([#2365](https://github.com/argoproj/argo-rollouts/issues/2365))
* **deps:** bump github.com/aws/aws-sdk-go-v2 from 1.16.16 to 1.17.0 ([#2364](https://github.com/argoproj/argo-rollouts/issues/2364))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch ([#2361](https://github.com/argoproj/argo-rollouts/issues/2361))
* **deps:** bump github.com/prometheus/client_model from 0.2.0 to 0.3.0 ([#2349](https://github.com/argoproj/argo-rollouts/issues/2349))
* **deps:** bump github.com/valyala/fasttemplate from 1.2.1 to 1.2.2 ([#2348](https://github.com/argoproj/argo-rollouts/issues/2348))
* **deps:** bump github.com/newrelic/newrelic-client-go ([#2344](https://github.com/argoproj/argo-rollouts/issues/2344))
* **deps:** bump google.golang.org/grpc from 1.50.0 to 1.50.1 ([#2340](https://github.com/argoproj/argo-rollouts/issues/2340))
* **deps:** bump github.com/prometheus/common from 0.36.0 to 0.37.0 ([#2143](https://github.com/argoproj/argo-rollouts/issues/2143))
* **deps:** bump github.com/sirupsen/logrus from 1.8.1 to 1.9.0 ([#2152](https://github.com/argoproj/argo-rollouts/issues/2152))
* **deps:** bump github.com/spf13/cobra from 1.5.0 to 1.6.0 ([#2313](https://github.com/argoproj/argo-rollouts/issues/2313))
* **deps:** bump github.com/newrelic/newrelic-client-go ([#2267](https://github.com/argoproj/argo-rollouts/issues/2267))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 ([#2307](https://github.com/argoproj/argo-rollouts/issues/2307))
* **deps:** bump docker/build-push-action from 2 to 3 ([#2306](https://github.com/argoproj/argo-rollouts/issues/2306))
* **deps:** bump docker/setup-buildx-action from 1 to 2 ([#2305](https://github.com/argoproj/argo-rollouts/issues/2305))
* **deps:** bump github.com/influxdata/influxdb-client-go/v2 ([#2304](https://github.com/argoproj/argo-rollouts/issues/2304))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 ([#2295](https://github.com/argoproj/argo-rollouts/issues/2295))
* **deps:** bump google.golang.org/protobuf from 1.28.0 to 1.28.1 ([#2296](https://github.com/argoproj/argo-rollouts/issues/2296))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch ([#2255](https://github.com/argoproj/argo-rollouts/issues/2255))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config ([#2294](https://github.com/argoproj/argo-rollouts/issues/2294))
* **deps:** bump google.golang.org/grpc from 1.47.0 to 1.50.0 ([#2293](https://github.com/argoproj/argo-rollouts/issues/2293))
* **deps:** bump docker/metadata-action from 3 to 4 ([#2292](https://github.com/argoproj/argo-rollouts/issues/2292))
* **deps:** bump github/codeql-action from 1 to 2 ([#2289](https://github.com/argoproj/argo-rollouts/issues/2289))
* **deps:** bump docker/login-action from 1 to 2 ([#2288](https://github.com/argoproj/argo-rollouts/issues/2288))
* **deps:** bump actions/setup-go from 2 to 3 ([#2287](https://github.com/argoproj/argo-rollouts/issues/2287))
* **deps:** bump dependabot/fetch-metadata from 1.3.3 to 1.3.4 ([#2286](https://github.com/argoproj/argo-rollouts/issues/2286))
* **deps:** bump EnricoMi/publish-unit-test-result-action from 1 to 2 ([#2285](https://github.com/argoproj/argo-rollouts/issues/2285))
* **deps:** bump actions/setup-python from 2 to 4.1.0 ([#2134](https://github.com/argoproj/argo-rollouts/issues/2134))
* **deps:** bump actions/cache from 2 to 3.0.1 ([#1940](https://github.com/argoproj/argo-rollouts/issues/1940))
* **deps:** bump docker/setup-qemu-action from 1 to 2 ([#2284](https://github.com/argoproj/argo-rollouts/issues/2284))
* **deps:** bump actions/checkout from 2 to 3.1.0 ([#2283](https://github.com/argoproj/argo-rollouts/issues/2283))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 ([#2486](https://github.com/argoproj/argo-rollouts/issues/2486))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config ([#2485](https://github.com/argoproj/argo-rollouts/issues/2485))

### Ci

* create stable tag for docs ([#2402](https://github.com/argoproj/argo-rollouts/issues/2402))
* fix some github actions warnings ([#2375](https://github.com/argoproj/argo-rollouts/issues/2375))
* add link to conventional pr check in pr template ([#2346](https://github.com/argoproj/argo-rollouts/issues/2346))
* auto generate changelog ([#2321](https://github.com/argoproj/argo-rollouts/issues/2321))
* adjust settings for stale pr and issues ([#2341](https://github.com/argoproj/argo-rollouts/issues/2341))
* fix pr lint check ([#2336](https://github.com/argoproj/argo-rollouts/issues/2336))
* add auto close to issues and prs ([#2319](https://github.com/argoproj/argo-rollouts/issues/2319))
* Add github action for PR Conventional Commits ([#2320](https://github.com/argoproj/argo-rollouts/issues/2320))

### Cleanup

* rename temlateref to templateref ([#2154](https://github.com/argoproj/argo-rollouts/issues/2154))

### Docs

* common questions for Rollbacks ([#2027](https://github.com/argoproj/argo-rollouts/issues/2027))
* correct syntax of canary setMirrorRoute's value ([#2431](https://github.com/argoproj/argo-rollouts/issues/2431))
* add artifact badge ([#2331](https://github.com/argoproj/argo-rollouts/issues/2331))
* Use new Google Analytics 4 site tag ([#2299](https://github.com/argoproj/argo-rollouts/issues/2299))
* add progressive delivery with gitops example for openshift ([#2400](https://github.com/argoproj/argo-rollouts/issues/2400))
* fix !important block typo ([#2372](https://github.com/argoproj/argo-rollouts/issues/2372))
* mention supported versions ([#2163](https://github.com/argoproj/argo-rollouts/issues/2163))
* Added blog post for minimize impact in Kubernetes using Progressive Delivery and customer side impact ([#2355](https://github.com/argoproj/argo-rollouts/issues/2355))
* add Opensurvey to USERS.md ([#2195](https://github.com/argoproj/argo-rollouts/issues/2195))
* fix typo in helm Argo rollouts ([#2442](https://github.com/argoproj/argo-rollouts/issues/2442))
* Explain upgrade process ([#2424](https://github.com/argoproj/argo-rollouts/issues/2424))
* Fixed read the docs rendering ([#2277](https://github.com/argoproj/argo-rollouts/issues/2277))
* Add traffic router support to readme ([#2444](https://github.com/argoproj/argo-rollouts/issues/2444))
* add OpsVerse as an official user (USERS.md) ([#2209](https://github.com/argoproj/argo-rollouts/issues/2209))
* Fix the controller annotation to enable data scrapping ([#2238](https://github.com/argoproj/argo-rollouts/issues/2238))
* Update release docs for versioned formula ([#2245](https://github.com/argoproj/argo-rollouts/issues/2245))
* Update docs for new openapi kustomize support ([#2216](https://github.com/argoproj/argo-rollouts/issues/2216))
* **trafficrouting:** fix docs warning to github style markdown ([#2342](https://github.com/argoproj/argo-rollouts/issues/2342))

### Feat

* Implement Issue [#1779](https://github.com/argoproj/argo-rollouts/issues/1779): add rollout.Spec.Strategy.Canary.MinPodsPerReplicaSet ([#2448](https://github.com/argoproj/argo-rollouts/issues/2448))
* Apache APISIX support. Fixes [#2395](https://github.com/argoproj/argo-rollouts/issues/2395) ([#2437](https://github.com/argoproj/argo-rollouts/issues/2437))
* rollback windows. Fixes [#574](https://github.com/argoproj/argo-rollouts/issues/574) ([#2394](https://github.com/argoproj/argo-rollouts/issues/2394))
* add support for getting the replicaset name via templating ([#2396](https://github.com/argoproj/argo-rollouts/issues/2396))
* Allow Traffic shaping through header based routing for ALB ([#2214](https://github.com/argoproj/argo-rollouts/issues/2214))
* Add support for spec.ingressClassName ([#2178](https://github.com/argoproj/argo-rollouts/issues/2178))
* Support TCP routes traffic splitting for Istio VirtualService ([#1659](https://github.com/argoproj/argo-rollouts/issues/1659))
* **cli:** dynamic shell completion for main resources names (rollouts, experiments, analysisrun) ([#2379](https://github.com/argoproj/argo-rollouts/issues/2379))
* **cli:** add port flag for dashboard command ([#2383](https://github.com/argoproj/argo-rollouts/issues/2383))
* **controller:** don't hardcode experiment ports; always create service ([#2397](https://github.com/argoproj/argo-rollouts/issues/2397))

### Fix

* dev build can set DEV_IMAGE=true ([#2440](https://github.com/argoproj/argo-rollouts/issues/2440))
* add patch verb to deployment resource ([#2407](https://github.com/argoproj/argo-rollouts/issues/2407))
* rootPath support so that it uses the embedded files system ([#2198](https://github.com/argoproj/argo-rollouts/issues/2198))
* set gopath in makefile ([#2398](https://github.com/argoproj/argo-rollouts/issues/2398))
* change completed condition so it only triggers on pod hash changes also adds an event for when it  does changes. ([#2203](https://github.com/argoproj/argo-rollouts/issues/2203))
* enable notifications without when condition ([#2231](https://github.com/argoproj/argo-rollouts/issues/2231))
* UI not redirecting on / ([#2252](https://github.com/argoproj/argo-rollouts/issues/2252))
* nil pointer while linting with basic canary and ingresses ([#2256](https://github.com/argoproj/argo-rollouts/issues/2256))
* **analysis:** Make AR End When Only Dry-Run Metrics Are Defined ([#2230](https://github.com/argoproj/argo-rollouts/issues/2230))
* **analysis:** Fix Analysis Terminal Decision For Dry-Run Metrics ([#2399](https://github.com/argoproj/argo-rollouts/issues/2399))
* **analysis:** Avoid Infinite Error Message Append For Failed Dry-Run Metrics ([#2182](https://github.com/argoproj/argo-rollouts/issues/2182))
* **cli:** nil pointer while linting  ([#2324](https://github.com/argoproj/argo-rollouts/issues/2324))
* **controller:**  Fix k8s clientset controller metrics. Fixes [#2139](https://github.com/argoproj/argo-rollouts/issues/2139) ([#2261](https://github.com/argoproj/argo-rollouts/issues/2261))
* **controller:** leader election preventing two controllers running and gracefully shutting down ([#2291](https://github.com/argoproj/argo-rollouts/issues/2291))
* **dashboard:** correct mime type is returned. Fixes: [#2290](https://github.com/argoproj/argo-rollouts/issues/2290) ([#2303](https://github.com/argoproj/argo-rollouts/issues/2303))
* **example:** correct docs when metrics got result empty ([#2309](https://github.com/argoproj/argo-rollouts/issues/2309))
* **metricprovider:** Support jsonBody for web metric provider Fixes [#2275](https://github.com/argoproj/argo-rollouts/issues/2275) ([#2312](https://github.com/argoproj/argo-rollouts/issues/2312))
* **trafficrouting:** Do not block the switch of service selectors for single pod failures ([#2441](https://github.com/argoproj/argo-rollouts/issues/2441))

### Fixes

* **controller:** istio dropping fields not defined in type ([#2268](https://github.com/argoproj/argo-rollouts/issues/2268))

### Test

* **controller:** add extra checks to TestWriteBackToInformer ([#2326](https://github.com/argoproj/argo-rollouts/issues/2326))


<a name="v1.3.3"></a>
## [v1.3.3](https://github.com/argoproj/argo-rollouts/compare/v1.3.2...v1.3.3) (2023-02-24)

### Chore

* make docs match branch now that we are supporting versions
* bump node version and set openssl-legacy-provider ([#2606](https://github.com/argoproj/argo-rollouts/issues/2606))
* disable docker sbom and attestations ([#2528](https://github.com/argoproj/argo-rollouts/issues/2528))

### Docs

* commit generated docs for readthedocs.org ([#2535](https://github.com/argoproj/argo-rollouts/issues/2535))
* fix rendering by upgrading deps ([#2495](https://github.com/argoproj/argo-rollouts/issues/2495))

### Fix

* support only tls in virtual services ([#2502](https://github.com/argoproj/argo-rollouts/issues/2502))


<a name="v1.3.2"></a>
## [v1.3.2](https://github.com/argoproj/argo-rollouts/compare/v1.3.1...v1.3.2) (2022-12-15)

### Chore

* fix checksum generation ([#2481](https://github.com/argoproj/argo-rollouts/issues/2481))

### Docs

* Fixed read the docs rendering ([#2277](https://github.com/argoproj/argo-rollouts/issues/2277))

### Fix

* **analysis:** Make AR End When Only Dry-Run Metrics Are Defined ([#2230](https://github.com/argoproj/argo-rollouts/issues/2230))
* **dashboard:** correct mime type is returned. Fixes: [#2290](https://github.com/argoproj/argo-rollouts/issues/2290) ([#2303](https://github.com/argoproj/argo-rollouts/issues/2303))
* **trafficrouting:** Do not block the switch of service selectors for single pod failures ([#2441](https://github.com/argoproj/argo-rollouts/issues/2441))


<a name="v1.3.1"></a>
## [v1.3.1](https://github.com/argoproj/argo-rollouts/compare/v1.3.0...v1.3.1) (2022-09-29)

### Fix

* nil pointer while linting with basic canary and ingresses ([#2256](https://github.com/argoproj/argo-rollouts/issues/2256))
* UI not redirecting on / ([#2252](https://github.com/argoproj/argo-rollouts/issues/2252))
* **controller:**  Fix k8s clientset controller metrics. Fixes [#2139](https://github.com/argoproj/argo-rollouts/issues/2139) ([#2261](https://github.com/argoproj/argo-rollouts/issues/2261))

### Fixes

* **controller:** istio dropping fields not defined in type ([#2268](https://github.com/argoproj/argo-rollouts/issues/2268))

