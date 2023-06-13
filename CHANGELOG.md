
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

