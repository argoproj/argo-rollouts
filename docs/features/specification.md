# Rollout Specification

The following describes all the available fields of a rollout:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: example-rollout-canary
spec:
  # Number of desired pods.
  # Defaults to 1.
  replicas: 5

  # Label selector for pods. Existing ReplicaSets whose pods are selected by
  # this will be the ones affected by this rollout. It must match the pod
  # template's labels.
  selector:
    matchLabels:
      app: guestbook

  # Template describes the pods that will be created. Same as deployment
  template:
    spec:
      containers:
      - name: guestbook
        image: argoproj/rollouts-demo:blue

  # Minimum number of seconds for which a newly created pod should be ready
  # without any of its container crashing, for it to be considered available.
  # Defaults to 0 (pod will be considered available as soon as it is ready)
  minReadySeconds: 30

  # The number of old ReplicaSets to retain.
  # Defaults to 10
  revisionHistoryLimit: 3

  # Pause allows a user to manually pause a rollout at any time. A rollout
  # will not advance through its steps while it is manually paused, but HPA
  # auto-scaling will still occur. Typically not explicitly set the manifest,
  # but controlled via tools (e.g. kubectl argo rollouts pause). If true at
  # initial creation of Rollout, replicas are not scaled up automatically
  # from zero unless manually promoted.
  paused: true

  # The maximum time in seconds in which a rollout must make progress during
  # an update, before it is considered to be failed. Argo Rollouts will
  # continue to process failed rollouts and a condition with a
  # ProgressDeadlineExceeded reason will be surfaced in the rollout status.
  # Note that progress will not be estimated during the time a rollout is
  # paused.
  # Defaults to 600s
  progressDeadlineSeconds: 600

  # UTC timestamp in which a Rollout should sequentially restart all of
  # its pods. Used by the `kubectl argo rollouts restart ROLLOUT` command.
  # The controller will ensure all pods have a creationTimestamp greater
  # than or equal to this value.
  restartAt: "2020-03-30T21:19:35Z"

  strategy:

    # Blue-green update strategy
    blueGreen:

      # Reference to service that the rollout modifies as the active service.
      # Required.
      activeService: active-service

      # Pre-promotion analysis run which performs analysis before the service
      # cutover. +optional
      prePromotionAnalysis:
        templates:
        - templateName: success-rate
        args:
        - name: service-name
          value: guestbook-svc.default.svc.cluster.local

      # Post-promotion analysis run which performs analysis after the service
      # cutover. +optional
      postPromotionAnalysis:
        templates:
        - templateName: success-rate
        args:
        - name: service-name
          value: guestbook-svc.default.svc.cluster.local

      # Name of the service that the rollout modifies as the preview service.
      # +optional
      previewService: preview-service

      # The number of replicas to run under the preview service before the
      # switchover. Once the rollout is resumed the new ReplicaSet will be fully
      # scaled up before the switch occurs +optional
      previewReplicaCount: 1

      # Indicates if the rollout should automatically promote the new ReplicaSet
      # to the active service or enter a paused state. If not specified, the
      # default value is true. +optional
      autoPromotionEnabled: false

      # Automatically promotes the current ReplicaSet to active after the
      # specified pause delay in seconds after the ReplicaSet becomes ready.
      # If omitted, the Rollout enters and remains in a paused state until
      # manually resumed by resetting spec.Paused to false. +optional
      autoPromotionSeconds: 30

      # Adds a delay before scaling down the previous ReplicaSet. If omitted,
      # the Rollout waits 30 seconds before scaling down the previous ReplicaSet.
      # A minimum of 30 seconds is recommended to ensure IP table propagation
      # across the nodes in a cluster.
      scaleDownDelaySeconds: 30

      # Limits the number of old RS that can run at once before getting scaled
      # down. Defaults to nil
      scaleDownDelayRevisionLimit: 2

      # Anti Affinity configuration between desired and previous ReplicaSet.
      # Only one must be specified
      antiAffinity:
        requiredDuringSchedulingIgnoredDuringExecution: {}
        preferredDuringSchedulingIgnoredDuringExecution:
          weight: 1 # Between 1 - 100

    # Canary update strategy
    canary:

      # Reference to a service which the controller will update to select
      # canary pods. Required for traffic routing.
      canaryService: canary-service

      # Reference to a service which the controller will update to select
      # stable pods. Required for traffic routing.
      stableService: stable-service

      # Metadata which will be attached to the canary pods. This metadata will
      # only exist during an update, since there are no canary pods in a fully
      # promoted rollout.
      canaryMetadata:
        annotations:
          role: canary
        labels:
          role: canary

      # metadata which will be attached to the stable pods
      stableMetadata:
        annotations:
          role: stable
        labels:
          role: stable

      # The maximum number of pods that can be unavailable during the update.
      # Value can be an absolute number (ex: 5) or a percentage of total pods
      # at the start of update (ex: 10%). Absolute number is calculated from
      # percentage by rounding down. This can not be 0 if  MaxSurge is 0. By
      # default, a fixed value of 1 is used. Example: when this is set to 30%,
      # the old RC can be scaled down by 30% immediately when the rolling
      # update starts. Once new pods are ready, old RC can be scaled down
      # further, followed by scaling up the new RC, ensuring that at least 70%
      # of original number of pods are available at all times during the
      # update. +optional
      maxUnavailable: 1

      # The maximum number of pods that can be scheduled above the original
      # number of pods. Value can be an absolute number (ex: 5) or a
      # percentage of total pods at the start of the update (ex: 10%). This
      # can not be 0 if MaxUnavailable is 0. Absolute number is calculated
      # from percentage by rounding up. By default, a value of 1 is used.
      # Example: when this is set to 30%, the new RC can be scaled up by 30%
      # immediately when the rolling update starts. Once old pods have been
      # killed, new RC can be scaled up further, ensuring that total number
      # of pods running at any time during the update is at most 130% of
      # original pods. +optional
      maxSurge: "20%"

      # Background analysis to run during a rollout update. Skipped upon
      # initial deploy of a rollout. +optional
      analysis:
        templates:
        - templateName: success-rate
        args:
        - name: service-name
          value: guestbook-svc.default.svc.cluster.local

        # valueFrom.podTemplateHashValue is a convenience to supply the
        # rollouts-pod-template-hash value of either the Stable ReplicaSet
        # or the Latest ReplicaSet
        - name: stable-hash
          valueFrom:
            podTemplateHashValue: Stable
        - name: latest-hash
          valueFrom:
            podTemplateHashValue: Latest

        # valueFrom.fieldRef allows metadata about the rollout to be
        # supplied as arguments to analysis.
        - name: region
          valueFrom:
            fieldRef:
              fieldPath: metadata.labels['region']

      # Steps define sequence of steps to take during an update of the
      # canary. Skipped upon initial deploy of a rollout. +optional
      steps:

      # Sets the ratio of canary ReplicaSet to 20%
      - setWeight: 20

      # Pauses the rollout for an hour. Supported units: s, m, h
      - pause:
          duration: 1h

      # Pauses indefinitely until manually resumed
      - pause: {}

      # set canary scale to a explicit count (supported only with trafficRouting)
      - setCanaryScale:
          replicas: 3

      # set canary scale to a percentage of spec.replicas
      # (supported only with trafficRouting)
      - setCanaryScale:
          weight: 25

      # set canary scale to match the canary traffic weight (default behavior)
      - setCanaryScale:
          matchTrafficWeight: true

      # an inline analysis step
      - analysis:
          templates:
          - templateName: success-rate

      # an inline experiment step
      - experiment:
          duration: 1h
          templates:
          - name: baseline
            specRef: stable
          - name: canary
            specRef: canary
          analyses:
          - name : mann-whitney
            templateName: mann-whitney

      # Anti-affinity configuration between desired and previous ReplicaSet.
      # Only one must be specified.
      antiAffinity:
        requiredDuringSchedulingIgnoredDuringExecution: {}
        preferredDuringSchedulingIgnoredDuringExecution:
          weight: 1 # Between 1 - 100

      # Traffic routing specifies the ingress controller or service mesh
      # configuration to achieve advanced traffic splitting. If omitted,
      # will achieve traffic split via a weighted replica counts between
      # the canary and stable ReplicaSet.
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
