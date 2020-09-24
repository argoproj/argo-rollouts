# Rollout Specification

The following describes all the available fields of a rollout:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: example-rollout-canary
spec:
  # Number of desired pods. This is a pointer to distinguish between explicit zero and not specified.
  # Defaults to 1.
  replicas: 5
  # Label selector for pods. Existing ReplicaSets whose pods are selected by this will be the ones
  # affected by this rollout. It must match the pod template's labels.
  selector:
    matchLabels:
      app: guestbook
  # Template describes the pods that will be created. Same as deployment
  template:
    spec:
      containers:
      - name: guestbook
        image: gcr.io/heptio-images/ks-guestbook-demo:0.1
  # Minimum number of seconds for which a newly created pod should be ready without any of its
  # container crashing, for it to be considered available.
  # Defaults to 0 (pod will be considered available as soon as it is ready)
  minReadySeconds: 30
  # The number of old ReplicaSets to retain.
  # Defaults to 10
  revisionHistoryLimit: 3
  # Pause allows a user to manually pause a rollout at any time. A rollout will not advance through
  # its steps while it is manually paused, but HPA auto-scaling will still occur.
  # Usually not used in the manifest, but if true at initial creation of Rollout, replicas are not scaled up automatically from zero unless manually promoted.
  paused: true
  # The maximum time in seconds in which a rollout must make progress during an update, before it is
  # considered to be failed. Argo Rollouts will continue to process failed rollouts and a condition
  # with a ProgressDeadlineExceeded reason will be surfaced in the rollout status. Note that
  # progress will not be estimated during the time a rollout is paused.
  # Defaults to 600s
  progressDeadlineSeconds: 600
  # UTC timestamp in which a Rollout should sequentially restart all of its pods. Used by the
  # `kubectl argo rollouts restart ROLLOUT` command. The controller will ensure all pods have a
  # creationTimestamp greater than or equal to this value.
  restartAt: "2020-03-30T21:19:35Z"
  # Deployment strategy to use during updates
  strategy:
    blueGreen:
      # Name of the service that the rollout modifies as the active service.
      activeService: active-service
      # Pre-promotion analysis run which performs analysis before the service cutover. +optional
      prePromotionAnalysis:
        templates:
        - templateName: success-rate
        # template arguments
        args:
        - name: service-name
          value: guestbook-svc.default.svc.cluster.local
      # Post-promotion analysis run which performs analysis after the service cutover. +optional
      postPromotionAnalysis:
        templates:
        - templateName: success-rate
        # template arguments
        args:
        - name: service-name
          value: guestbook-svc.default.svc.cluster.local
      # Name of the service that the rollout modifies as the preview service. +optional
      previewService: preview-service
      # The number of replicas to run under the preview service before the switchover. Once the rollout is resumed the new replicaset will be full scaled up before the switch occurs +optional
      previewReplicaCount: 1
      # Indicates if the rollout should automatically promote the new ReplicaSet to the active service or enter a paused state. If not specified, the default value is true. +optional
      autoPromotionEnabled: false
      # Automatically promotes the current ReplicaSet to active after the specified pause delay in seconds after the ReplicaSet becomes ready. If omitted, the Rollout enters and remains in a paused state until manually resumed by resetting spec.Paused to false. +optional
      autoPromotionSeconds: 30
      # Adds a delay before scaling down the previous replicaset. If omitted, the Rollout waits 30 seconds before scaling down the previous ReplicaSet. A minimum of 30 seconds is recommended to ensure IP table propagation across the nodes in a cluster. See https://github.com/argoproj/argo-rollouts/issues/19#issuecomment-476329960 for more information
      scaleDownDelaySeconds: 30
      # Limits the number of old RS that can run at once before getting scaled down. Defaults to nil
      scaleDownDelayRevisionLimit: 2
      # Anti Affinity configuration between desired and previous replicaset. Only one must be specified
      antiAffinity:
        requiredDuringSchedulingIgnoredDuringExecution: {}
        preferredDuringSchedulingIgnoredDuringExecution:
          weight: 1 # Between 1 - 100
    canary:
      # CanaryService holds the name of a service which selects pods with canary version and don't select any pods with stable version. +optional
      canaryService: canary-service
      # StableService holds the name of a service which selects pods with stable version and don't select any pods with canary version. +optional
      stableService: stable-service
      # The maximum number of pods that can be unavailable during the update. Value can be an absolute number (ex: 5) or a percentage of total pods at the start of update (ex: 10%). Absolute number is calculated from percentage by rounding down. This can not be 0 if MaxSurge is 0. By default, a fixed value of 1 is used. Example: when this is set to 30%, the old RC can be scaled down by 30% immediately when the rolling update starts. Once new pods are ready, old RC can be scaled down further, followed by scaling up the new RC, ensuring that at least 70% of original number of pods are available at all times during the update. +optional
      maxUnavailable: 1
      # The maximum number of pods that can be scheduled above the original number of pods. Value can be an absolute number (ex: 5) or a percentage of total pods at the start of the update (ex: 10%). This can not be 0 if MaxUnavailable is 0. Absolute number is calculated from percentage by rounding up. By default, a value of 1 is used. Example: when this is set to 30%, the new RC can be scaled up by 30% immediately when the rolling update starts. Once old pods have been killed, new RC can be scaled up further, ensuring that total number of pods running at any time during the update is at most 130% of original pods. +optional
      maxSurge: "20%"
      # Background analysis to run during the rollout
      analysis:
        templates:
        - templateName: success-rate
        # template arguments
        args:
        - name: service-name
          value: guestbook-svc.default.svc.cluster.local
      # Define the order of phases to execute the canary deployment +optional
      steps:
        # Sets the ratio of new replicasets to 20%
      - setWeight: 20 
        # Pauses the rollout for an hour
      - pause:
          duration: 1h # One hour
      - setWeight: 40
        # Sets .spec.paused to true and waits until the field is changed back
      - pause: {}
      # Anti Affinity configuration between desired and previous replicaset. Only one must be specified
      antiAffinity:
        requiredDuringSchedulingIgnoredDuringExecution: {}
        preferredDuringSchedulingIgnoredDuringExecution:
          weight: 1 # Between 1 - 100
      # Traffic routing specifies ingress controller or service mesh configuration to achieve
      # advanced traffic splitting. If omitted, will achieve traffic split via a weighted
      # replica counts between the canary and stable ReplicaSet.
      trafficRouting:
        # Istio traffic routing configuration
        istio:
          virtualService: 
            name: rollout-vsvc  # required
            routes:
            - primary # At least one route is required
        # NGINX Ingress Controller routing configuration
        nginx:
          stableIngress: primary-ingress  # required
          annotationPrefix: customingress.nginx.ingress.kubernetes.io # optional
          additionalIngressAnnotations:   # optional
            canary-by-header: X-Canary
            canary-by-header-value: iwantsit
        # ALB Ingress Controller routing configuration
        alb:
           ingress: ingress  # required
           servicePort: 443  # required
           annotationPrefix: custom.alb.ingress.kubernetes.io # optional
        # Service Mesh Interface routing configuration
        smi:
         rootService: root-svc # optional
         trafficSplitName: rollout-example-traffic-split # optional

status:
  pauseConditions:
  - reason: StepPause
    startTime: 2019-10-00T1234
  - reason: BlueGreenPause
    startTime: 2019-10-00T1234
  - reason: AnalysisRunInconclusive
    startTime: 2019-10-00T1234 
```
