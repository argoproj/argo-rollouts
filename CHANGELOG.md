
<a name="v1.7.0"></a>
## [v1.7.0](https://github.com/argoproj/argo-rollouts/compare/v1.7.0-rc1...v1.7.0) (2024-06-12)

### Fix

* verify the weight of the alb at the end of the rollout ([#3627](https://github.com/argoproj/argo-rollouts/issues/3627))
* when Rollout has pingpong and stable/canary service defined, only alb traffic management uses pingpong. ([#3628](https://github.com/argoproj/argo-rollouts/issues/3628))
* protocol missing in ambassador canary mapping creation. Fixes  [#3593](https://github.com/argoproj/argo-rollouts/issues/3593) ([#3603](https://github.com/argoproj/argo-rollouts/issues/3603))
* rs conflict with fallback to patch ([#3559](https://github.com/argoproj/argo-rollouts/issues/3559))
* **controller:** Corrects the logic of comparing sha256 has. Fixes [#3519](https://github.com/argoproj/argo-rollouts/issues/3519) ([#3520](https://github.com/argoproj/argo-rollouts/issues/3520))


<a name="v1.7.0-rc1"></a>
## [v1.7.0-rc1](https://github.com/argoproj/argo-rollouts/compare/v1.6.6...v1.7.0-rc1) (2024-04-03)

### Build

* **deps:** always resolve momentjs version 2.29.4 ([#3182](https://github.com/argoproj/argo-rollouts/issues/3182))

### Chore

* fix PodSecurity warning ([#3424](https://github.com/argoproj/argo-rollouts/issues/3424))
* add WeLab Bank to users.md ([#2996](https://github.com/argoproj/argo-rollouts/issues/2996))
* change file name for readthedocs compatibility ([#2999](https://github.com/argoproj/argo-rollouts/issues/2999))
* Update users doc with CircleCI ([#3028](https://github.com/argoproj/argo-rollouts/issues/3028))
* bump k8s versions to 1.29 ([#3494](https://github.com/argoproj/argo-rollouts/issues/3494))
* updating getCanaryConfigId to be more efficient with better error handling ([#3070](https://github.com/argoproj/argo-rollouts/issues/3070))
* add missing rollout fields ([#3062](https://github.com/argoproj/argo-rollouts/issues/3062))
* upgrade cosign ([#3139](https://github.com/argoproj/argo-rollouts/issues/3139))
* add OpenSSF Scorecard badge ([#3154](https://github.com/argoproj/argo-rollouts/issues/3154))
* add test for reconcileEphemeralMetadata() ([#3163](https://github.com/argoproj/argo-rollouts/issues/3163))
* leave the validation of setHeaderRoute to the plugin when plugins is not empty. ([#2898](https://github.com/argoproj/argo-rollouts/issues/2898))
* fix lint errors reported by golangci-lint ([#3458](https://github.com/argoproj/argo-rollouts/issues/3458))
* fix unit test data races ([#3478](https://github.com/argoproj/argo-rollouts/issues/3478)) ([#3479](https://github.com/argoproj/argo-rollouts/issues/3479))
* added organization to users.md ([#3481](https://github.com/argoproj/argo-rollouts/issues/3481))
* set webpack hashFunction to modern sha256, remove legacy-provider. Fixes [#2609](https://github.com/argoproj/argo-rollouts/issues/2609) ([#3475](https://github.com/argoproj/argo-rollouts/issues/3475))
* remove year from codegen license  ([#3282](https://github.com/argoproj/argo-rollouts/issues/3282))
* update follow-redirects to 1.15.5 ([#3314](https://github.com/argoproj/argo-rollouts/issues/3314))
* add logging context around replicaset updates ([#3326](https://github.com/argoproj/argo-rollouts/issues/3326))
* bump notification engine lib ([#3327](https://github.com/argoproj/argo-rollouts/issues/3327))
* change controller's deploy strategy to RollingUpdate due to leader election ([#3334](https://github.com/argoproj/argo-rollouts/issues/3334))
* Add exception to `requireCanaryStableServices` to disable validation when using the `hashicorp/consul` plugin ([#3339](https://github.com/argoproj/argo-rollouts/issues/3339))
* Update notifications engine to 7a06976 ([#3384](https://github.com/argoproj/argo-rollouts/issues/3384))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 from 1.30.4 to 1.30.5 ([#3491](https://github.com/argoproj/argo-rollouts/issues/3491))
* **deps:** bump golang.org/x/oauth2 from 0.17.0 to 0.18.0 ([#3422](https://github.com/argoproj/argo-rollouts/issues/3422))
* **deps:** bump softprops/action-gh-release from 2.0.3 to 2.0.4 ([#3442](https://github.com/argoproj/argo-rollouts/issues/3442))
* **deps:** bump softprops/action-gh-release from 2.0.2 to 2.0.3 ([#3440](https://github.com/argoproj/argo-rollouts/issues/3440))
* **deps:** bump softprops/action-gh-release from 1 to 2 ([#3438](https://github.com/argoproj/argo-rollouts/issues/3438))
* **deps:** bump docker/build-push-action from 5.1.0 to 5.2.0 ([#3439](https://github.com/argoproj/argo-rollouts/issues/3439))
* **deps:** bump docker/setup-buildx-action from 3.1.0 to 3.2.0 ([#3449](https://github.com/argoproj/argo-rollouts/issues/3449))
* **deps:** bump google.golang.org/grpc from 1.62.0 to 1.62.1 ([#3426](https://github.com/argoproj/argo-rollouts/issues/3426))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.27.4 to 1.27.5 ([#3421](https://github.com/argoproj/argo-rollouts/issues/3421))
* **deps:** bump github.com/stretchr/testify from 1.8.4 to 1.9.0 ([#3419](https://github.com/argoproj/argo-rollouts/issues/3419))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.27.0 to 1.27.4 ([#3410](https://github.com/argoproj/argo-rollouts/issues/3410))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 from 1.27.0 to 1.30.1 ([#3399](https://github.com/argoproj/argo-rollouts/issues/3399))
* **deps:** bump google.golang.org/grpc from 1.61.0 to 1.62.0 ([#3404](https://github.com/argoproj/argo-rollouts/issues/3404))
* **deps:** bump docker/setup-buildx-action from 3.0.0 to 3.1.0 ([#3406](https://github.com/argoproj/argo-rollouts/issues/3406))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.33.0 to 1.36.1 ([#3400](https://github.com/argoproj/argo-rollouts/issues/3400))
* **deps:** bump codecov/codecov-action from 4.0.1 to 4.1.0 ([#3403](https://github.com/argoproj/argo-rollouts/issues/3403))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 from 1.30.1 to 1.30.3 ([#3447](https://github.com/argoproj/argo-rollouts/issues/3447))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.26.6 to 1.27.0 ([#3368](https://github.com/argoproj/argo-rollouts/issues/3368))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.32.2 to 1.33.0 ([#3363](https://github.com/argoproj/argo-rollouts/issues/3363))
* **deps:** bump docker/login-action from 3.0.0 to 3.1.0 ([#3443](https://github.com/argoproj/argo-rollouts/issues/3443))
* **deps:** bump golang.org/x/oauth2 from 0.16.0 to 0.17.0 ([#3357](https://github.com/argoproj/argo-rollouts/issues/3357))
* **deps:** bump golangci/golangci-lint-action from 3 to 4 ([#3359](https://github.com/argoproj/argo-rollouts/issues/3359))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 from 1.26.7 to 1.27.0 ([#3341](https://github.com/argoproj/argo-rollouts/issues/3341))
* **deps:** bump peter-evans/create-pull-request from 5 to 6 ([#3342](https://github.com/argoproj/argo-rollouts/issues/3342))
* **deps:** bump sigstore/cosign-installer from 3.3.0 to 3.4.0 ([#3343](https://github.com/argoproj/argo-rollouts/issues/3343))
* **deps:** bump codecov/codecov-action from 3.1.5 to 4.0.1 ([#3347](https://github.com/argoproj/argo-rollouts/issues/3347))
* **deps:** bump github.com/evanphx/json-patch/v5 from 5.8.1 to 5.9.0 ([#3335](https://github.com/argoproj/argo-rollouts/issues/3335))
* **deps:** bump docker/build-push-action from 5.2.0 to 5.3.0 ([#3448](https://github.com/argoproj/argo-rollouts/issues/3448))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.26.5 to 1.26.6 ([#3322](https://github.com/argoproj/argo-rollouts/issues/3322))
* **deps:** bump github.com/evanphx/json-patch/v5 from 5.8.0 to 5.8.1 ([#3312](https://github.com/argoproj/argo-rollouts/issues/3312))
* **deps:** bump codecov/codecov-action from 3.1.4 to 3.1.5 ([#3330](https://github.com/argoproj/argo-rollouts/issues/3330))
* **deps:** bump slsa-framework/slsa-github-generator from 1.9.0 to 1.9.1 ([#3456](https://github.com/argoproj/argo-rollouts/issues/3456))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.36.1 to 1.36.3 ([#3452](https://github.com/argoproj/argo-rollouts/issues/3452))
* **deps:** bump google.golang.org/grpc from 1.60.1 to 1.61.0 ([#3325](https://github.com/argoproj/argo-rollouts/issues/3325))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.26.4 to 1.26.5 ([#3319](https://github.com/argoproj/argo-rollouts/issues/3319))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.26.3 to 1.26.4 ([#3313](https://github.com/argoproj/argo-rollouts/issues/3313))
* **deps:** bump actions/cache from 3 to 4 ([#3315](https://github.com/argoproj/argo-rollouts/issues/3315))
* **deps:** bump slsa-framework/slsa-github-generator from 1.9.1 to 1.10.0 ([#3462](https://github.com/argoproj/argo-rollouts/issues/3462))
* **deps:** bump github.com/evanphx/json-patch/v5 from 5.7.0 to 5.8.0 ([#3309](https://github.com/argoproj/argo-rollouts/issues/3309))
* **deps:** bump golang.org/x/oauth2 from 0.15.0 to 0.16.0 ([#3294](https://github.com/argoproj/argo-rollouts/issues/3294))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.32.1 to 1.32.2 ([#3288](https://github.com/argoproj/argo-rollouts/issues/3288))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.26.2 to 1.26.3 ([#3289](https://github.com/argoproj/argo-rollouts/issues/3289))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 from 1.26.6 to 1.26.7 ([#3290](https://github.com/argoproj/argo-rollouts/issues/3290))
* **deps:** bump github.com/aws/aws-sdk-go-v2 from 1.24.0 to 1.24.1 ([#3291](https://github.com/argoproj/argo-rollouts/issues/3291))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 from 1.30.3 to 1.30.4 ([#3461](https://github.com/argoproj/argo-rollouts/issues/3461))
* **deps:** bump google.golang.org/protobuf from 1.31.0 to 1.32.0 ([#3273](https://github.com/argoproj/argo-rollouts/issues/3273))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.26.1 to 1.26.2 ([#3268](https://github.com/argoproj/argo-rollouts/issues/3268))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 from 1.26.5 to 1.26.6 ([#3269](https://github.com/argoproj/argo-rollouts/issues/3269))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.32.0 to 1.32.1 ([#3270](https://github.com/argoproj/argo-rollouts/issues/3270))
* **deps:** bump google.golang.org/grpc from 1.60.0 to 1.60.1 ([#3260](https://github.com/argoproj/argo-rollouts/issues/3260))
* **deps:** bump github/codeql-action from 2 to 3 ([#3252](https://github.com/argoproj/argo-rollouts/issues/3252))
* **deps:** bump actions/upload-artifact from 3 to 4 ([#3255](https://github.com/argoproj/argo-rollouts/issues/3255))
* **deps:** bump sigstore/cosign-installer from 3.2.0 to 3.3.0 ([#3245](https://github.com/argoproj/argo-rollouts/issues/3245))
* **deps:** bump google.golang.org/grpc from 1.59.0 to 1.60.0 ([#3246](https://github.com/argoproj/argo-rollouts/issues/3246))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.26.0 to 1.26.1 ([#3241](https://github.com/argoproj/argo-rollouts/issues/3241))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 from 1.26.4 to 1.26.5 ([#3240](https://github.com/argoproj/argo-rollouts/issues/3240))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.31.4 to 1.32.0 ([#3239](https://github.com/argoproj/argo-rollouts/issues/3239))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.25.12 to 1.26.0 ([#3236](https://github.com/argoproj/argo-rollouts/issues/3236))
* **deps:** bump codecov/codecov-action from 4.1.0 to 4.1.1 ([#3476](https://github.com/argoproj/argo-rollouts/issues/3476))
* **deps:** bump github.com/influxdata/influxdb-client-go/v2 from 2.12.4 to 2.13.0 ([#3217](https://github.com/argoproj/argo-rollouts/issues/3217))
* **deps:** bump actions/stale from 8 to 9 ([#3232](https://github.com/argoproj/argo-rollouts/issues/3232))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.31.3 to 1.31.4 ([#3235](https://github.com/argoproj/argo-rollouts/issues/3235))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 from 1.26.3 to 1.26.4 ([#3234](https://github.com/argoproj/argo-rollouts/issues/3234))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.25.11 to 1.25.12 ([#3230](https://github.com/argoproj/argo-rollouts/issues/3230))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.31.2 to 1.31.3 ([#3226](https://github.com/argoproj/argo-rollouts/issues/3226))
* **deps:** bump actions/setup-python from 4 to 5 ([#3227](https://github.com/argoproj/argo-rollouts/issues/3227))
* **deps:** bump actions/setup-go from 4.1.0 to 5.0.0 ([#3228](https://github.com/argoproj/argo-rollouts/issues/3228))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 from 1.26.2 to 1.26.3 ([#3229](https://github.com/argoproj/argo-rollouts/issues/3229))
* **deps:** Bump k8s dependencies to v1.26.11 ([#3211](https://github.com/argoproj/argo-rollouts/issues/3211))
* **deps:** bump argo-ui and fix browser console errors ([#3212](https://github.com/argoproj/argo-rollouts/issues/3212))
* **deps:** bump docker/build-push-action from 5.0.0 to 5.1.0 ([#3178](https://github.com/argoproj/argo-rollouts/issues/3178))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.25.10 to 1.25.11 ([#3206](https://github.com/argoproj/argo-rollouts/issues/3206))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 from 1.26.1 to 1.26.2 ([#3207](https://github.com/argoproj/argo-rollouts/issues/3207))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.31.1 to 1.31.2 ([#3208](https://github.com/argoproj/argo-rollouts/issues/3208))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.30.5 to 1.31.1 ([#3201](https://github.com/argoproj/argo-rollouts/issues/3201))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 from 1.25.2 to 1.26.1 ([#3203](https://github.com/argoproj/argo-rollouts/issues/3203))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.25.8 to 1.25.10 ([#3204](https://github.com/argoproj/argo-rollouts/issues/3204))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.25.5 to 1.25.8 ([#3191](https://github.com/argoproj/argo-rollouts/issues/3191))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 from 1.24.3 to 1.25.2 ([#3192](https://github.com/argoproj/argo-rollouts/issues/3192))
* **deps:** bump golang.org/x/oauth2 from 0.13.0 to 0.15.0 ([#3187](https://github.com/argoproj/argo-rollouts/issues/3187))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.30.3 to 1.30.5 ([#3193](https://github.com/argoproj/argo-rollouts/issues/3193))
* **deps:** bump github.com/antonmedv/expr from 1.15.4 to 1.15.5 ([#3186](https://github.com/argoproj/argo-rollouts/issues/3186))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.30.1 to 1.30.3 ([#3179](https://github.com/argoproj/argo-rollouts/issues/3179))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 from 1.24.0 to 1.24.3 ([#3180](https://github.com/argoproj/argo-rollouts/issues/3180))
* **deps:** bump github.com/influxdata/influxdb-client-go/v2 from 2.12.3 to 2.12.4 ([#3150](https://github.com/argoproj/argo-rollouts/issues/3150))
* **deps:** bump github.com/antonmedv/expr from 1.15.3 to 1.15.4 ([#3184](https://github.com/argoproj/argo-rollouts/issues/3184))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.23.0 to 1.25.5 ([#3183](https://github.com/argoproj/argo-rollouts/issues/3183))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.30.0 to 1.30.1 ([#3166](https://github.com/argoproj/argo-rollouts/issues/3166))
* **deps:** bump github.com/hashicorp/go-plugin from 1.5.2 to 1.6.0 ([#3167](https://github.com/argoproj/argo-rollouts/issues/3167))
* **deps:** update golang to 1.21 ([#3482](https://github.com/argoproj/argo-rollouts/issues/3482))
* **deps:** bump github.com/bombsimon/logrusr/v4 from 4.0.0 to 4.1.0 ([#3151](https://github.com/argoproj/argo-rollouts/issues/3151))
* **deps:** bump github.com/spf13/cobra from 1.7.0 to 1.8.0 ([#3152](https://github.com/argoproj/argo-rollouts/issues/3152))
* **deps:** bump sigstore/cosign-installer from 3.1.2 to 3.2.0 ([#3158](https://github.com/argoproj/argo-rollouts/issues/3158))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.22.0 to 1.23.0 ([#3161](https://github.com/argoproj/argo-rollouts/issues/3161))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.27.5 to 1.27.9 ([#3469](https://github.com/argoproj/argo-rollouts/issues/3469))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.28.0 to 1.30.0 ([#3144](https://github.com/argoproj/argo-rollouts/issues/3144))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 from 1.22.0 to 1.24.0 ([#3143](https://github.com/argoproj/argo-rollouts/issues/3143))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.20.0 to 1.22.0 ([#3149](https://github.com/argoproj/argo-rollouts/issues/3149))
* **deps:** bump google.golang.org/protobuf from 1.32.0 to 1.33.0 ([#3429](https://github.com/argoproj/argo-rollouts/issues/3429))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.19.1 to 1.20.0 ([#3135](https://github.com/argoproj/argo-rollouts/issues/3135))
* **deps:** bump github.com/aws/aws-sdk-go-v2 from 1.21.2 to 1.22.0 ([#3136](https://github.com/argoproj/argo-rollouts/issues/3136))
* **deps:** bump sigs.k8s.io/yaml from 1.3.0 to 1.4.0 ([#3122](https://github.com/argoproj/argo-rollouts/issues/3122))
* **deps:** bump google.golang.org/grpc from 1.58.3 to 1.59.0 ([#3113](https://github.com/argoproj/argo-rollouts/issues/3113))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 from 1.21.6 to 1.22.0 ([#3127](https://github.com/argoproj/argo-rollouts/issues/3127))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.19.0 to 1.19.1 ([#3123](https://github.com/argoproj/argo-rollouts/issues/3123))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.27.9 to 1.28.0 ([#3124](https://github.com/argoproj/argo-rollouts/issues/3124))
* **deps:** bump golang.org/x/oauth2 from 0.10.0 to 0.13.0 ([#3107](https://github.com/argoproj/argo-rollouts/issues/3107))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.18.45 to 1.19.0 ([#3109](https://github.com/argoproj/argo-rollouts/issues/3109))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.18.44 to 1.18.45 ([#3101](https://github.com/argoproj/argo-rollouts/issues/3101))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 from 1.21.4 to 1.21.6 ([#3100](https://github.com/argoproj/argo-rollouts/issues/3100))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.27.8 to 1.27.9 ([#3102](https://github.com/argoproj/argo-rollouts/issues/3102))
* **deps:** bump github.com/aws/aws-sdk-go-v2 from 1.21.1 to 1.21.2 ([#3103](https://github.com/argoproj/argo-rollouts/issues/3103))
* **deps:** bump github.com/aws/smithy-go from 1.20.1 to 1.20.2 ([#3488](https://github.com/argoproj/argo-rollouts/issues/3488))
* **deps:** bump google.golang.org/grpc from 1.58.2 to 1.58.3 ([#3098](https://github.com/argoproj/argo-rollouts/issues/3098))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.18.43 to 1.18.44 ([#3099](https://github.com/argoproj/argo-rollouts/issues/3099))
* **deps:** bump github.com/aws/aws-sdk-go-v2 from 1.21.0 to 1.21.1 ([#3085](https://github.com/argoproj/argo-rollouts/issues/3085))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.27.7 to 1.27.8 ([#3086](https://github.com/argoproj/argo-rollouts/issues/3086))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.18.42 to 1.18.43 ([#3072](https://github.com/argoproj/argo-rollouts/issues/3072))
* **deps:** bump github.com/hashicorp/go-plugin from 1.5.1 to 1.5.2 ([#3056](https://github.com/argoproj/argo-rollouts/issues/3056))
* **deps:** bump github.com/prometheus/common from 0.42.0 to 0.51.1 ([#3468](https://github.com/argoproj/argo-rollouts/issues/3468))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.18.41 to 1.18.42 ([#3055](https://github.com/argoproj/argo-rollouts/issues/3055))
* **deps:** bump github.com/antonmedv/expr from 1.15.2 to 1.15.3 ([#3046](https://github.com/argoproj/argo-rollouts/issues/3046))
* **deps:** bump docker/setup-qemu-action from 2.2.0 to 3.0.0 ([#3031](https://github.com/argoproj/argo-rollouts/issues/3031))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.18.39 to 1.18.41 ([#3047](https://github.com/argoproj/argo-rollouts/issues/3047))
* **deps:** bump google.golang.org/grpc from 1.58.0 to 1.58.2 ([#3050](https://github.com/argoproj/argo-rollouts/issues/3050))
* **deps:** bump google.golang.org/grpc from 1.57.0 to 1.58.0 ([#3023](https://github.com/argoproj/argo-rollouts/issues/3023))
* **deps:** bump github.com/evanphx/json-patch/v5 from 5.6.0 to 5.7.0 ([#3030](https://github.com/argoproj/argo-rollouts/issues/3030))
* **deps:** bump docker/metadata-action from 4 to 5 ([#3032](https://github.com/argoproj/argo-rollouts/issues/3032))
* **deps:** bump docker/build-push-action from 4.1.1 to 5.0.0 ([#3033](https://github.com/argoproj/argo-rollouts/issues/3033))
* **deps:** bump docker/setup-buildx-action from 2.10.0 to 3.0.0 ([#3034](https://github.com/argoproj/argo-rollouts/issues/3034))
* **deps:** bump docker/login-action from 2.2.0 to 3.0.0 ([#3035](https://github.com/argoproj/argo-rollouts/issues/3035))
* **deps:** bump github.com/antonmedv/expr from 1.15.1 to 1.15.2 ([#3036](https://github.com/argoproj/argo-rollouts/issues/3036))
* **deps:** bump github.com/aws/aws-sdk-go-v2 from 1.26.0 to 1.26.1 ([#3490](https://github.com/argoproj/argo-rollouts/issues/3490))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 from 1.21.3 to 1.21.4 ([#3025](https://github.com/argoproj/argo-rollouts/issues/3025))
* **deps:** bump github.com/hashicorp/go-plugin from 1.5.0 to 1.5.1 ([#3017](https://github.com/argoproj/argo-rollouts/issues/3017))
* **deps:** bump github.com/antonmedv/expr from 1.13.0 to 1.15.1 ([#3024](https://github.com/argoproj/argo-rollouts/issues/3024))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.18.38 to 1.18.39 ([#3018](https://github.com/argoproj/argo-rollouts/issues/3018))
* **deps:** bump actions/checkout from 3 to 4 ([#3012](https://github.com/argoproj/argo-rollouts/issues/3012))
* **deps:** bump sigstore/cosign-installer from 3.1.1 to 3.1.2 ([#3011](https://github.com/argoproj/argo-rollouts/issues/3011))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.18.37 to 1.18.38 ([#3002](https://github.com/argoproj/argo-rollouts/issues/3002))
* **deps:** bump github.com/hashicorp/go-plugin from 1.4.10 to 1.5.0 ([#2995](https://github.com/argoproj/argo-rollouts/issues/2995))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.36.3 to 1.37.0 ([#3489](https://github.com/argoproj/argo-rollouts/issues/3489))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.27.9 to 1.27.10 ([#3492](https://github.com/argoproj/argo-rollouts/issues/3492))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.27.6 to 1.27.7 ([#2990](https://github.com/argoproj/argo-rollouts/issues/2990))
* **deps:** bump docker/setup-buildx-action from 2.9.1 to 2.10.0 ([#2994](https://github.com/argoproj/argo-rollouts/issues/2994))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 from 1.21.0 to 1.21.3 ([#2977](https://github.com/argoproj/argo-rollouts/issues/2977))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.18.36 to 1.18.37 ([#2984](https://github.com/argoproj/argo-rollouts/issues/2984))
* **deps:** bump slsa-framework/slsa-github-generator from 1.8.0 to 1.9.0 ([#2983](https://github.com/argoproj/argo-rollouts/issues/2983))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.18.33 to 1.18.36 ([#2978](https://github.com/argoproj/argo-rollouts/issues/2978))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.27.2 to 1.27.6 ([#2979](https://github.com/argoproj/argo-rollouts/issues/2979))

### Docs

* more best practices ([#3484](https://github.com/argoproj/argo-rollouts/issues/3484))
* typo in BlueGreen ([#3463](https://github.com/argoproj/argo-rollouts/issues/3463))
* minor readability on migration ([#3427](https://github.com/argoproj/argo-rollouts/issues/3427))
* added Consul plugin support to website ([#3362](https://github.com/argoproj/argo-rollouts/issues/3362))
* Update shell autocompletion instructions ([#3377](https://github.com/argoproj/argo-rollouts/issues/3377))
* Update Changelog ([#3365](https://github.com/argoproj/argo-rollouts/issues/3365))
* Guides for popular use-cases ([#3346](https://github.com/argoproj/argo-rollouts/issues/3346))
* Update Changelog ([#3328](https://github.com/argoproj/argo-rollouts/issues/3328))
* Fixed the key for headers in prometheus based argo analysis ([#3306](https://github.com/argoproj/argo-rollouts/issues/3306))
* mention archival of the SMI spec ([#3263](https://github.com/argoproj/argo-rollouts/issues/3263))
* Update Changelog ([#3244](https://github.com/argoproj/argo-rollouts/issues/3244))
* Update Changelog ([#3214](https://github.com/argoproj/argo-rollouts/issues/3214))
* Update Changelog ([#2952](https://github.com/argoproj/argo-rollouts/issues/2952))
* fix typo in smi.md ([#3160](https://github.com/argoproj/argo-rollouts/issues/3160))
* Update Changelog ([#3148](https://github.com/argoproj/argo-rollouts/issues/3148))
* add Gateway-API integration information to README.md ([#2985](https://github.com/argoproj/argo-rollouts/issues/2985))
* add CONTRIBUTING.md at root of repo, directing to docs/ ([#3121](https://github.com/argoproj/argo-rollouts/issues/3121))
* Ensure image not present between incomplete sentence. ([#3079](https://github.com/argoproj/argo-rollouts/issues/3079))
* clarify external clusters ([#3058](https://github.com/argoproj/argo-rollouts/issues/3058))
* Update Changelog ([#3021](https://github.com/argoproj/argo-rollouts/issues/3021))
* replace `patchesStrategicMerge` with `patches` in tests/docs ([#3010](https://github.com/argoproj/argo-rollouts/issues/3010))
* update all ingress objects to networking.k8s.io/v1 ([#3005](https://github.com/argoproj/argo-rollouts/issues/3005))
* Remove rogue apostrophe in features/analysis.md ([#3001](https://github.com/argoproj/argo-rollouts/issues/3001))
* add contour integration information to README.md ([#2980](https://github.com/argoproj/argo-rollouts/issues/2980))
* **analysis:** Add note about availability of new datadog v2 functionality ([#3131](https://github.com/argoproj/argo-rollouts/issues/3131))
* **deps:** Specify minimum kustomize version ([#3199](https://github.com/argoproj/argo-rollouts/issues/3199))

### Feat

* Reference AnalysisTemplates inside an AnalysisTemplate ([#3353](https://github.com/argoproj/argo-rollouts/issues/3353))
* add command args for plugin ([#2992](https://github.com/argoproj/argo-rollouts/issues/2992))
* expose secrets for notification templates ([#3455](https://github.com/argoproj/argo-rollouts/issues/3455)) ([#3466](https://github.com/argoproj/argo-rollouts/issues/3466))
* ping pong support for istio ([#3371](https://github.com/argoproj/argo-rollouts/issues/3371))
* display init container images on the rollout dashboard ([#3473](https://github.com/argoproj/argo-rollouts/issues/3473))
* add Analysis run to rollout  notifications ([#3296](https://github.com/argoproj/argo-rollouts/issues/3296))
* add the max traffic weight support for the traffic routing (nginx/plugins). ([#3215](https://github.com/argoproj/argo-rollouts/issues/3215))
* allow analysis run to use separate kubeconfig for jobs ([#3350](https://github.com/argoproj/argo-rollouts/issues/3350))
* Support AnalysisRunMetadata and Dryrun for experiments via Rollout ([#3213](https://github.com/argoproj/argo-rollouts/issues/3213))
* allow setting traefik versions ([#3348](https://github.com/argoproj/argo-rollouts/issues/3348))
* support ability to run only the analysis controller ([#3336](https://github.com/argoproj/argo-rollouts/issues/3336))
* Support OAuth2 for prometheus and web providers ([#3038](https://github.com/argoproj/argo-rollouts/issues/3038))
* Add support for aggregator type in DataDog metric provider  ([#3293](https://github.com/argoproj/argo-rollouts/issues/3293))
* add analysis modal ([#3174](https://github.com/argoproj/argo-rollouts/issues/3174))
* automatically scale down Deployment after migrating to Rollout ([#3111](https://github.com/argoproj/argo-rollouts/issues/3111))
* Rollouts UI List View Refresh ([#3118](https://github.com/argoproj/argo-rollouts/issues/3118))
* **analysis:** add ttlStrategy on AnalysisRun for garbage collecting stale AnalysisRun automatically ([#3324](https://github.com/argoproj/argo-rollouts/issues/3324))
* **dashboard:** improve pods visibility ([#3483](https://github.com/argoproj/argo-rollouts/issues/3483))
* **trafficrouting:** use values array for multiple accepted values under same header name ([#2974](https://github.com/argoproj/argo-rollouts/issues/2974))

### Fix

* set formatter for klog logger ([#3493](https://github.com/argoproj/argo-rollouts/issues/3493))
* fix the issue that when max weight is 100000000, and the replicas> 20,  the trafficWeightToReplicas will return negative value. ([#3474](https://github.com/argoproj/argo-rollouts/issues/3474))
* analysis step should be ignored after promote ([#3016](https://github.com/argoproj/argo-rollouts/issues/3016))
* job metrics owner ref when using custom job kubeconfig/ns ([#3425](https://github.com/argoproj/argo-rollouts/issues/3425))
* Add the GOPATH to the go-to-protobuf command ([#3022](https://github.com/argoproj/argo-rollouts/issues/3022))
* prevent hot loop when fully promoted rollout is aborted ([#3064](https://github.com/argoproj/argo-rollouts/issues/3064))
* include the correct response error in the plugin init error message ([#3388](https://github.com/argoproj/argo-rollouts/issues/3388))
* append weighted destination only when weight is mentioned  ([#2734](https://github.com/argoproj/argo-rollouts/issues/2734))
* stuck rollout when 2nd deployment happens before 1st finishes ([#3354](https://github.com/argoproj/argo-rollouts/issues/3354))
* do not require pod readiness when switching desired service selector on abort ([#3338](https://github.com/argoproj/argo-rollouts/issues/3338))
* log rs name when update fails ([#3318](https://github.com/argoproj/argo-rollouts/issues/3318))
* keep rs inormer updated upon updating labels and annotations ([#3321](https://github.com/argoproj/argo-rollouts/issues/3321))
* updates to replicas and pod template at the same time causes rollout to get stuck ([#3272](https://github.com/argoproj/argo-rollouts/issues/3272))
* canary step analysis run wasn't terminated as keep running after promote action being called. Fixes [#3220](https://github.com/argoproj/argo-rollouts/issues/3220) ([#3221](https://github.com/argoproj/argo-rollouts/issues/3221))
* make sure we use the updated rs when we write back to informer ([#3237](https://github.com/argoproj/argo-rollouts/issues/3237))
* conflict on updates to replicaset revision ([#3216](https://github.com/argoproj/argo-rollouts/issues/3216))
* rollouts getting stuck due to bad rs informer updates ([#3200](https://github.com/argoproj/argo-rollouts/issues/3200))
* missing notification on error ([#3076](https://github.com/argoproj/argo-rollouts/issues/3076))
* istio destionationrule subsets enforcement ([#3126](https://github.com/argoproj/argo-rollouts/issues/3126))
* docs require build.os to be defined ([#3133](https://github.com/argoproj/argo-rollouts/issues/3133))
* rollback to stable with dynamicStableScale could overwhelm stable pods ([#3077](https://github.com/argoproj/argo-rollouts/issues/3077))
* inopportune scaling events would lose some status fields ([#3060](https://github.com/argoproj/argo-rollouts/issues/3060))
* codegen was missed ([#3104](https://github.com/argoproj/argo-rollouts/issues/3104))
* keep rs informer updated ([#3091](https://github.com/argoproj/argo-rollouts/issues/3091))
* bump notification-engine to fix double send on self server notifications ([#3095](https://github.com/argoproj/argo-rollouts/issues/3095))
* revert repo change to expr ([#3094](https://github.com/argoproj/argo-rollouts/issues/3094))
* Replace antonmedv/expr with expr-lang/expr ([#3090](https://github.com/argoproj/argo-rollouts/issues/3090))
* Revert "fix: istio destionationrule subsets enforcement ([#3126](https://github.com/argoproj/argo-rollouts/issues/3126))" ([#3147](https://github.com/argoproj/argo-rollouts/issues/3147))
* sync notification controller configmaps/secrets first ([#3075](https://github.com/argoproj/argo-rollouts/issues/3075))
* **controller:** don't timeout rollout when still waiting for scale down delay ([#3417](https://github.com/argoproj/argo-rollouts/issues/3417))
* **controller:** treat spec.canary.analysis.template empty list as spec.canary.analysis not set ([#3446](https://github.com/argoproj/argo-rollouts/issues/3446))
* **controller:** prevent negative vsvc weights on a replica scaledown following a canary abort for istio trafficrouting ([#3467](https://github.com/argoproj/argo-rollouts/issues/3467))
* **controller:** rollback should skip all steps to active rs within RollbackWindow ([#2953](https://github.com/argoproj/argo-rollouts/issues/2953))
* **controller:** typo fix ("Secrete" -> "Secret") in secret informer ([#2965](https://github.com/argoproj/argo-rollouts/issues/2965))
* **metricprovider:** support Datadog v2 API Fixes [#2813](https://github.com/argoproj/argo-rollouts/issues/2813) ([#2997](https://github.com/argoproj/argo-rollouts/issues/2997))

### Refactor

* rename interface{} => any ([#3000](https://github.com/argoproj/argo-rollouts/issues/3000))

### Test

* add unit tests for maxSurge=0, replicas=1 ([#3375](https://github.com/argoproj/argo-rollouts/issues/3375))


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

* quote golang version string to not use go 1.2.2 ([#2915](https://github.com/argoproj/argo-rollouts/issues/2915))
* bump gotestsum and fix flakey test causing nil channel send ([#2934](https://github.com/argoproj/argo-rollouts/issues/2934))
* Update test and related docs for plugin name standard ([#2728](https://github.com/argoproj/argo-rollouts/issues/2728))
* bump k8s deps to v0.25.8 ([#2712](https://github.com/argoproj/argo-rollouts/issues/2712))
* add zachaller as lead in owers file ([#2759](https://github.com/argoproj/argo-rollouts/issues/2759))
* add unit test ([#2798](https://github.com/argoproj/argo-rollouts/issues/2798))
* add make help cmd ([#2854](https://github.com/argoproj/argo-rollouts/issues/2854))
* Add tests for pause functionality in rollout package ([#2772](https://github.com/argoproj/argo-rollouts/issues/2772))
* bump golang to 1.20 ([#2910](https://github.com/argoproj/argo-rollouts/issues/2910))
* **deps:** bump actions/setup-go from 4.0.1 to 4.1.0 ([#2947](https://github.com/argoproj/argo-rollouts/issues/2947))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.18.30 to 1.18.31 ([#2924](https://github.com/argoproj/argo-rollouts/issues/2924))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.18.29 to 1.18.30 ([#2919](https://github.com/argoproj/argo-rollouts/issues/2919))
* **deps:** bump github.com/aws/aws-sdk-go-v2 from 1.19.0 to 1.19.1 ([#2920](https://github.com/argoproj/argo-rollouts/issues/2920))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.26.3 to 1.27.0 ([#2922](https://github.com/argoproj/argo-rollouts/issues/2922))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.18.31 to 1.18.32 ([#2928](https://github.com/argoproj/argo-rollouts/issues/2928))
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
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.27.0 to 1.27.1 ([#2927](https://github.com/argoproj/argo-rollouts/issues/2927))
* **deps:** bump google.golang.org/grpc from 1.55.0 to 1.56.1 ([#2856](https://github.com/argoproj/argo-rollouts/issues/2856))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 from 1.19.14 to 1.20.1 ([#2926](https://github.com/argoproj/argo-rollouts/issues/2926))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 from 1.19.12 to 1.19.13 ([#2847](https://github.com/argoproj/argo-rollouts/issues/2847))
* **deps:** bump actions/setup-go from 3.5.0 to 4.0.1 ([#2849](https://github.com/argoproj/argo-rollouts/issues/2849))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.18.26 to 1.18.27 ([#2844](https://github.com/argoproj/argo-rollouts/issues/2844))
* **deps:** bump github.com/prometheus/client_golang from 1.15.1 to 1.16.0 ([#2846](https://github.com/argoproj/argo-rollouts/issues/2846))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.26.1 to 1.26.2 ([#2848](https://github.com/argoproj/argo-rollouts/issues/2848))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 from 1.19.11 to 1.19.12 ([#2839](https://github.com/argoproj/argo-rollouts/issues/2839))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.26.0 to 1.26.1 ([#2840](https://github.com/argoproj/argo-rollouts/issues/2840))
* **deps:** bump sigstore/cosign-installer from 3.0.5 to 3.1.0 ([#2858](https://github.com/argoproj/argo-rollouts/issues/2858))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.18.25 to 1.18.26 ([#2841](https://github.com/argoproj/argo-rollouts/issues/2841))
* **deps:** bump docker/build-push-action from 4.0.0 to 4.1.0 ([#2832](https://github.com/argoproj/argo-rollouts/issues/2832))
* **deps:** bump github.com/sirupsen/logrus from 1.9.2 to 1.9.3 ([#2821](https://github.com/argoproj/argo-rollouts/issues/2821))
* **deps:** bump github.com/hashicorp/go-plugin from 1.4.9 to 1.4.10 ([#2822](https://github.com/argoproj/argo-rollouts/issues/2822))
* **deps:** bump github.com/stretchr/testify from 1.8.3 to 1.8.4 ([#2817](https://github.com/argoproj/argo-rollouts/issues/2817))
* **deps:** bump github.com/sirupsen/logrus from 1.9.1 to 1.9.2 ([#2789](https://github.com/argoproj/argo-rollouts/issues/2789))
* **deps:** bump github.com/stretchr/testify from 1.8.2 to 1.8.3 ([#2796](https://github.com/argoproj/argo-rollouts/issues/2796))
* **deps:** bump slsa-framework/slsa-github-generator from 1.7.0 to 1.8.0 ([#2936](https://github.com/argoproj/argo-rollouts/issues/2936))
* **deps:** bump sigstore/cosign-installer from 3.0.3 to 3.0.5 ([#2788](https://github.com/argoproj/argo-rollouts/issues/2788))
* **deps:** bump docker/build-push-action from 4.1.0 to 4.1.1 ([#2837](https://github.com/argoproj/argo-rollouts/issues/2837))
* **deps:** bump github.com/sirupsen/logrus from 1.9.0 to 1.9.1 ([#2784](https://github.com/argoproj/argo-rollouts/issues/2784))
* **deps:** bump codecov/codecov-action from 3.1.3 to 3.1.4 ([#2782](https://github.com/argoproj/argo-rollouts/issues/2782))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.18.24 to 1.18.25 ([#2770](https://github.com/argoproj/argo-rollouts/issues/2770))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.18.23 to 1.18.24 ([#2768](https://github.com/argoproj/argo-rollouts/issues/2768))
* **deps:** bump google.golang.org/grpc from 1.54.0 to 1.55.0 ([#2763](https://github.com/argoproj/argo-rollouts/issues/2763))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.18.22 to 1.18.23 ([#2756](https://github.com/argoproj/argo-rollouts/issues/2756))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 from 1.20.1 to 1.20.2 ([#2941](https://github.com/argoproj/argo-rollouts/issues/2941))
* **deps:** replace `github.com/ghodss/yaml` with `sigs.k8s.io/yaml` ([#2681](https://github.com/argoproj/argo-rollouts/issues/2681))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.25.10 to 1.26.0 ([#2755](https://github.com/argoproj/argo-rollouts/issues/2755))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 from 1.19.10 to 1.19.11 ([#2757](https://github.com/argoproj/argo-rollouts/issues/2757))
* **deps:** bump github.com/prometheus/client_golang from 1.15.0 to 1.15.1 ([#2754](https://github.com/argoproj/argo-rollouts/issues/2754))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.18.21 to 1.18.22 ([#2746](https://github.com/argoproj/argo-rollouts/issues/2746))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.25.9 to 1.25.10 ([#2745](https://github.com/argoproj/argo-rollouts/issues/2745))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.27.1 to 1.27.2 ([#2944](https://github.com/argoproj/argo-rollouts/issues/2944))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 from 1.19.9 to 1.19.10 ([#2747](https://github.com/argoproj/argo-rollouts/issues/2747))
* **deps:** bump codecov/codecov-action from 3.1.2 to 3.1.3 ([#2735](https://github.com/argoproj/argo-rollouts/issues/2735))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.18.32 to 1.18.33 ([#2943](https://github.com/argoproj/argo-rollouts/issues/2943))
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

* mirroring support in Traefik is not implemented yet ([#2904](https://github.com/argoproj/argo-rollouts/issues/2904))
* update contributions.md to include k3d as recommended cluster, add details on e2e test setup, and update kubectl install link. Fixes [#1750](https://github.com/argoproj/argo-rollouts/issues/1750) ([#1867](https://github.com/argoproj/argo-rollouts/issues/1867))
* fix minor mistakes in Migrating to Deployments ([#2270](https://github.com/argoproj/argo-rollouts/issues/2270))
* Update docs of Rollout spec to add active/previewMetadata ([#2833](https://github.com/argoproj/argo-rollouts/issues/2833))
* Update datadog.md - clarify formulas [#2813](https://github.com/argoproj/argo-rollouts/issues/2813) ([#2819](https://github.com/argoproj/argo-rollouts/issues/2819))
* support for Kong ingress ([#2820](https://github.com/argoproj/argo-rollouts/issues/2820))
* Fix AWS App Mesh getting started documentation to avoid connection pooling problems ([#2814](https://github.com/argoproj/argo-rollouts/issues/2814))
* Update Changelog ([#2807](https://github.com/argoproj/argo-rollouts/issues/2807))
* use correct capitalization for "Datadog" in navigation sidebar ([#2809](https://github.com/argoproj/argo-rollouts/issues/2809))
* Fix typo in header routing specification docs ([#2808](https://github.com/argoproj/argo-rollouts/issues/2808))
* support for Google Cloud Load balancers ([#2803](https://github.com/argoproj/argo-rollouts/issues/2803))
* Show how plugins are loaded ([#2801](https://github.com/argoproj/argo-rollouts/issues/2801))
* Add gateway API link, fix Contour plugin naming ([#2787](https://github.com/argoproj/argo-rollouts/issues/2787))
* Add some details around running locally to make things clearer new contributors ([#2786](https://github.com/argoproj/argo-rollouts/issues/2786))
* Add docs for Amazon Managed Prometheus ([#2777](https://github.com/argoproj/argo-rollouts/issues/2777))
* Update Changelog ([#2765](https://github.com/argoproj/argo-rollouts/issues/2765))
* copy argo cd docs drop down fix ([#2731](https://github.com/argoproj/argo-rollouts/issues/2731))
* Add contour trafficrouter plugin ([#2729](https://github.com/argoproj/argo-rollouts/issues/2729))
* fix link to plugins for traffic routers ([#2719](https://github.com/argoproj/argo-rollouts/issues/2719))
* Update Changelog ([#2683](https://github.com/argoproj/argo-rollouts/issues/2683))
* **analysis:** fix use stringData in the examples ([#2715](https://github.com/argoproj/argo-rollouts/issues/2715))
* **example:** Add example on how to execute subset of e2e tests ([#2867](https://github.com/argoproj/argo-rollouts/issues/2867))
* **example:** interval requires count ([#2690](https://github.com/argoproj/argo-rollouts/issues/2690))

### Feat

* Send informer add k8s event ([#2834](https://github.com/argoproj/argo-rollouts/issues/2834))
* enable self service notification support ([#2930](https://github.com/argoproj/argo-rollouts/issues/2930))
* support prometheus headers ([#2937](https://github.com/argoproj/argo-rollouts/issues/2937))
* Add insecure option for Prometheus. Fixes [#2913](https://github.com/argoproj/argo-rollouts/issues/2913) ([#2914](https://github.com/argoproj/argo-rollouts/issues/2914))
* Add prometheus timeout ([#2893](https://github.com/argoproj/argo-rollouts/issues/2893))
* Support Multiple ALB Ingresses ([#2639](https://github.com/argoproj/argo-rollouts/issues/2639))
* add merge key to analysis template ([#2842](https://github.com/argoproj/argo-rollouts/issues/2842))
* retain TLS configuration for canary ingresses in the nginx integration. Fixes [#1134](https://github.com/argoproj/argo-rollouts/issues/1134) ([#2679](https://github.com/argoproj/argo-rollouts/issues/2679))
* **analysis:** Adds rollout Spec.Selector.MatchLabels to AnalysisRun. Fixes [#2888](https://github.com/argoproj/argo-rollouts/issues/2888) ([#2903](https://github.com/argoproj/argo-rollouts/issues/2903))
* **controller:** Add custom metadata support for AnalysisRun. Fixes [#2740](https://github.com/argoproj/argo-rollouts/issues/2740) ([#2743](https://github.com/argoproj/argo-rollouts/issues/2743))
* **dashboard:** Refresh Rollouts dashboard UI ([#2723](https://github.com/argoproj/argo-rollouts/issues/2723))
* **metricprovider:** allow user to define metrics.provider.job.metadata ([#2762](https://github.com/argoproj/argo-rollouts/issues/2762))

### Fix

* make new alb fullName field  optional for backward compatability ([#2806](https://github.com/argoproj/argo-rollouts/issues/2806))
* cloudwatch metrics provider multiple dimensions ([#2932](https://github.com/argoproj/argo-rollouts/issues/2932))
*  rollout not modify the VirtualService whit setHeaderRoute step with workloadRef ([#2797](https://github.com/argoproj/argo-rollouts/issues/2797))
* get new httpRoutesI after removeRoute() to avoid duplicates. Fixes [#2769](https://github.com/argoproj/argo-rollouts/issues/2769) ([#2887](https://github.com/argoproj/argo-rollouts/issues/2887))
* change logic of analysis run to better handle errors ([#2695](https://github.com/argoproj/argo-rollouts/issues/2695))
* istio dropping fields during removing of managed routes ([#2692](https://github.com/argoproj/argo-rollouts/issues/2692))
* resolve args to metric in garbage collection function ([#2843](https://github.com/argoproj/argo-rollouts/issues/2843))
* properly wrap Datadog API v2 request body ([#2771](https://github.com/argoproj/argo-rollouts/issues/2771)) ([#2775](https://github.com/argoproj/argo-rollouts/issues/2775))
* add required ingress permission ([#2933](https://github.com/argoproj/argo-rollouts/issues/2933))
* **analysis:** Adding field in YAML to provide region for Sigv4 signing.  ([#2794](https://github.com/argoproj/argo-rollouts/issues/2794))
* **analysis:** Graphite query - remove whitespaces ([#2752](https://github.com/argoproj/argo-rollouts/issues/2752))
* **analysis:** Graphite metric provider - index out of range [0] with length 0 ([#2751](https://github.com/argoproj/argo-rollouts/issues/2751))
* **controller:** Remove name label from some k8s client metrics on events and replicasets ([#2851](https://github.com/argoproj/argo-rollouts/issues/2851))
* **controller:** Fix for rollouts getting stuck in loop ([#2689](https://github.com/argoproj/argo-rollouts/issues/2689))
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
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.25.6 to 1.25.7 ([#2682](https://github.com/argoproj/argo-rollouts/issues/2682))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.18.15 to 1.18.16 ([#2652](https://github.com/argoproj/argo-rollouts/issues/2652))
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
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 from 1.19.6 to 1.19.7 ([#2672](https://github.com/argoproj/argo-rollouts/issues/2672))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.25.3 to 1.25.4 ([#2617](https://github.com/argoproj/argo-rollouts/issues/2617))
* **deps:** bump github.com/antonmedv/expr from 1.12.0 to 1.12.1 ([#2619](https://github.com/argoproj/argo-rollouts/issues/2619))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 from 1.19.4 to 1.19.5 ([#2616](https://github.com/argoproj/argo-rollouts/issues/2616))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 from 1.19.3 to 1.19.4 ([#2612](https://github.com/argoproj/argo-rollouts/issues/2612))
* **deps:** bump github.com/prometheus/common from 0.39.0 to 0.40.0 ([#2611](https://github.com/argoproj/argo-rollouts/issues/2611))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.18.13 to 1.18.14 ([#2614](https://github.com/argoproj/argo-rollouts/issues/2614))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.25.2 to 1.25.3 ([#2615](https://github.com/argoproj/argo-rollouts/issues/2615))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.18.14 to 1.18.15 ([#2618](https://github.com/argoproj/argo-rollouts/issues/2618))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config from 1.18.17 to 1.18.19 ([#2673](https://github.com/argoproj/argo-rollouts/issues/2673))
* **deps:** bump imjasonh/setup-crane from 0.2 to 0.3 ([#2600](https://github.com/argoproj/argo-rollouts/issues/2600))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch from 1.25.5 to 1.25.6 ([#2671](https://github.com/argoproj/argo-rollouts/issues/2671))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config ([#2593](https://github.com/argoproj/argo-rollouts/issues/2593))
* **deps:** bump github.com/antonmedv/expr from 1.12.3 to 1.12.5 ([#2670](https://github.com/argoproj/argo-rollouts/issues/2670))
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

* update GetTargetGroupMetadata to call DescribeTags in batches ([#2570](https://github.com/argoproj/argo-rollouts/issues/2570))
* switch service selector back to stable on canary service when aborted ([#2540](https://github.com/argoproj/argo-rollouts/issues/2540))
* change log generator to only add CHANGELOG.md ([#2626](https://github.com/argoproj/argo-rollouts/issues/2626))
* Rollback change on service creation with weightless experiments ([#2624](https://github.com/argoproj/argo-rollouts/issues/2624))
* flakey TestWriteBackToInformer test ([#2621](https://github.com/argoproj/argo-rollouts/issues/2621))
* remove outdated ioutil package dependencies ([#2583](https://github.com/argoproj/argo-rollouts/issues/2583))
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

* Add Yotpo to USERS.md
* upgrade golang to 1.19 ([#2219](https://github.com/argoproj/argo-rollouts/issues/2219))
* remove deprecated -i for go build ([#2047](https://github.com/argoproj/argo-rollouts/issues/2047))
* rename the examples/trafffic-management directory to istio ([#2315](https://github.com/argoproj/argo-rollouts/issues/2315))
* update stable tag conditionally ([#2480](https://github.com/argoproj/argo-rollouts/issues/2480))
* fix checksum generation ([#2481](https://github.com/argoproj/argo-rollouts/issues/2481))
* add optum to users list ([#2466](https://github.com/argoproj/argo-rollouts/issues/2466))
* use docker login to sign images ([#2479](https://github.com/argoproj/argo-rollouts/issues/2479))
* use correct image for plugin container ([#2478](https://github.com/argoproj/argo-rollouts/issues/2478))
* Add example for istio-subset-split ([#2318](https://github.com/argoproj/argo-rollouts/issues/2318))
* add deprecation notice for rollout_phase in docs ([#2377](https://github.com/argoproj/argo-rollouts/issues/2377)) ([#2378](https://github.com/argoproj/argo-rollouts/issues/2378))
* sign container images and checksum assets ([#2334](https://github.com/argoproj/argo-rollouts/issues/2334))
* **cli:** add darwin arm64 to build and release ([#2264](https://github.com/argoproj/argo-rollouts/issues/2264))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch ([#2487](https://github.com/argoproj/argo-rollouts/issues/2487))
* **deps:** bump github.com/prometheus/common from 0.37.0 to 0.38.0 ([#2468](https://github.com/argoproj/argo-rollouts/issues/2468))
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
* **deps:** bump github.com/prometheus/client_golang ([#2469](https://github.com/argoproj/argo-rollouts/issues/2469))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch ([#2404](https://github.com/argoproj/argo-rollouts/issues/2404))
* **deps:** bump notification engine ([#2470](https://github.com/argoproj/argo-rollouts/issues/2470))
* **deps:** bump codecov/codecov-action from 2.1.0 to 3.1.1 ([#2251](https://github.com/argoproj/argo-rollouts/issues/2251))
* **deps:** bump github.com/prometheus/common from 0.38.0 to 0.39.0 ([#2476](https://github.com/argoproj/argo-rollouts/issues/2476))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch ([#2477](https://github.com/argoproj/argo-rollouts/issues/2477))
* **deps:** bump github.com/aws/aws-sdk-go-v2 from 1.17.2 to 1.17.3 ([#2484](https://github.com/argoproj/argo-rollouts/issues/2484))
* **deps:** bump dependabot/fetch-metadata from 1.3.4 to 1.3.5 ([#2390](https://github.com/argoproj/argo-rollouts/issues/2390))
* **deps:** bump imjasonh/setup-crane from 0.1 to 0.2 ([#2387](https://github.com/argoproj/argo-rollouts/issues/2387))
* **deps:** upgrade ui deps to fix high security cve's ([#2345](https://github.com/argoproj/argo-rollouts/issues/2345))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 ([#2406](https://github.com/argoproj/argo-rollouts/issues/2406))
* **deps:** bump actions/upload-artifact from 2 to 3 ([#1973](https://github.com/argoproj/argo-rollouts/issues/1973))
* **deps:** bump github.com/influxdata/influxdb-client-go/v2 ([#2381](https://github.com/argoproj/argo-rollouts/issues/2381))
* **deps:** bump github.com/spf13/cobra from 1.6.0 to 1.6.1 ([#2370](https://github.com/argoproj/argo-rollouts/issues/2370))
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/cloudwatch ([#2366](https://github.com/argoproj/argo-rollouts/issues/2366))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config ([#2367](https://github.com/argoproj/argo-rollouts/issues/2367))
* **deps:** bump github.com/aws/aws-sdk-go-v2 from 1.17.0 to 1.17.1 ([#2369](https://github.com/argoproj/argo-rollouts/issues/2369))
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
* **deps:** bump github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 ([#2486](https://github.com/argoproj/argo-rollouts/issues/2486))
* **deps:** bump docker/login-action from 1 to 2 ([#2288](https://github.com/argoproj/argo-rollouts/issues/2288))
* **deps:** bump actions/setup-go from 2 to 3 ([#2287](https://github.com/argoproj/argo-rollouts/issues/2287))
* **deps:** bump dependabot/fetch-metadata from 1.3.3 to 1.3.4 ([#2286](https://github.com/argoproj/argo-rollouts/issues/2286))
* **deps:** bump EnricoMi/publish-unit-test-result-action from 1 to 2 ([#2285](https://github.com/argoproj/argo-rollouts/issues/2285))
* **deps:** bump actions/setup-python from 2 to 4.1.0 ([#2134](https://github.com/argoproj/argo-rollouts/issues/2134))
* **deps:** bump actions/cache from 2 to 3.0.1 ([#1940](https://github.com/argoproj/argo-rollouts/issues/1940))
* **deps:** bump docker/setup-qemu-action from 1 to 2 ([#2284](https://github.com/argoproj/argo-rollouts/issues/2284))
* **deps:** bump actions/checkout from 2 to 3.1.0 ([#2283](https://github.com/argoproj/argo-rollouts/issues/2283))
* **deps:** bump github.com/aws/aws-sdk-go-v2/config ([#2485](https://github.com/argoproj/argo-rollouts/issues/2485))
* **deps:** bump github/codeql-action from 1 to 2 ([#2289](https://github.com/argoproj/argo-rollouts/issues/2289))

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

* Add traffic router support to readme ([#2444](https://github.com/argoproj/argo-rollouts/issues/2444))
* fix typo in helm Argo rollouts ([#2442](https://github.com/argoproj/argo-rollouts/issues/2442))
* correct syntax of canary setMirrorRoute's value ([#2431](https://github.com/argoproj/argo-rollouts/issues/2431))
* Explain upgrade process ([#2424](https://github.com/argoproj/argo-rollouts/issues/2424))
* add progressive delivery with gitops example for openshift ([#2400](https://github.com/argoproj/argo-rollouts/issues/2400))
* fix !important block typo ([#2372](https://github.com/argoproj/argo-rollouts/issues/2372))
* mention supported versions ([#2163](https://github.com/argoproj/argo-rollouts/issues/2163))
* Added blog post for minimize impact in Kubernetes using Progressive Delivery and customer side impact ([#2355](https://github.com/argoproj/argo-rollouts/issues/2355))
* Update docs for new openapi kustomize support ([#2216](https://github.com/argoproj/argo-rollouts/issues/2216))
* add artifact badge ([#2331](https://github.com/argoproj/argo-rollouts/issues/2331))
* Use new Google Analytics 4 site tag ([#2299](https://github.com/argoproj/argo-rollouts/issues/2299))
* Fixed read the docs rendering ([#2277](https://github.com/argoproj/argo-rollouts/issues/2277))
* common questions for Rollbacks ([#2027](https://github.com/argoproj/argo-rollouts/issues/2027))
* add OpsVerse as an official user (USERS.md) ([#2209](https://github.com/argoproj/argo-rollouts/issues/2209))
* Fix the controller annotation to enable data scrapping ([#2238](https://github.com/argoproj/argo-rollouts/issues/2238))
* Update release docs for versioned formula ([#2245](https://github.com/argoproj/argo-rollouts/issues/2245))
* add Opensurvey to USERS.md ([#2195](https://github.com/argoproj/argo-rollouts/issues/2195))
* **trafficrouting:** fix docs warning to github style markdown ([#2342](https://github.com/argoproj/argo-rollouts/issues/2342))

### Feat

* Implement Issue [#1779](https://github.com/argoproj/argo-rollouts/issues/1779): add rollout.Spec.Strategy.Canary.MinPodsPerReplicaSet ([#2448](https://github.com/argoproj/argo-rollouts/issues/2448))
* Apache APISIX support. Fixes [#2395](https://github.com/argoproj/argo-rollouts/issues/2395) ([#2437](https://github.com/argoproj/argo-rollouts/issues/2437))
* rollback windows. Fixes [#574](https://github.com/argoproj/argo-rollouts/issues/574) ([#2394](https://github.com/argoproj/argo-rollouts/issues/2394))
* Support TCP routes traffic splitting for Istio VirtualService ([#1659](https://github.com/argoproj/argo-rollouts/issues/1659))
* add support for getting the replicaset name via templating ([#2396](https://github.com/argoproj/argo-rollouts/issues/2396))
* Allow Traffic shaping through header based routing for ALB ([#2214](https://github.com/argoproj/argo-rollouts/issues/2214))
* Add support for spec.ingressClassName ([#2178](https://github.com/argoproj/argo-rollouts/issues/2178))
* **cli:** dynamic shell completion for main resources names (rollouts, experiments, analysisrun) ([#2379](https://github.com/argoproj/argo-rollouts/issues/2379))
* **cli:** add port flag for dashboard command ([#2383](https://github.com/argoproj/argo-rollouts/issues/2383))
* **controller:** don't hardcode experiment ports; always create service ([#2397](https://github.com/argoproj/argo-rollouts/issues/2397))

### Fix

* set gopath in makefile ([#2398](https://github.com/argoproj/argo-rollouts/issues/2398))
* dev build can set DEV_IMAGE=true ([#2440](https://github.com/argoproj/argo-rollouts/issues/2440))
* add patch verb to deployment resource ([#2407](https://github.com/argoproj/argo-rollouts/issues/2407))
* rootPath support so that it uses the embedded files system ([#2198](https://github.com/argoproj/argo-rollouts/issues/2198))
* change completed condition so it only triggers on pod hash changes also adds an event for when it  does changes. ([#2203](https://github.com/argoproj/argo-rollouts/issues/2203))
* enable notifications without when condition ([#2231](https://github.com/argoproj/argo-rollouts/issues/2231))
* UI not redirecting on / ([#2252](https://github.com/argoproj/argo-rollouts/issues/2252))
* nil pointer while linting with basic canary and ingresses ([#2256](https://github.com/argoproj/argo-rollouts/issues/2256))
* **analysis:** Fix Analysis Terminal Decision For Dry-Run Metrics ([#2399](https://github.com/argoproj/argo-rollouts/issues/2399))
* **analysis:** Make AR End When Only Dry-Run Metrics Are Defined ([#2230](https://github.com/argoproj/argo-rollouts/issues/2230))
* **analysis:** Avoid Infinite Error Message Append For Failed Dry-Run Metrics ([#2182](https://github.com/argoproj/argo-rollouts/issues/2182))
* **cli:** nil pointer while linting  ([#2324](https://github.com/argoproj/argo-rollouts/issues/2324))
* **controller:** leader election preventing two controllers running and gracefully shutting down ([#2291](https://github.com/argoproj/argo-rollouts/issues/2291))
* **controller:**  Fix k8s clientset controller metrics. Fixes [#2139](https://github.com/argoproj/argo-rollouts/issues/2139) ([#2261](https://github.com/argoproj/argo-rollouts/issues/2261))
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
