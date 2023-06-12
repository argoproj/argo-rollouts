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
  analysis:
    # limits the number of successful analysis runs and experiments to be stored in a history
    # Defaults to 5.
    successfulRunHistoryLimit: 10
    # limits the number of unsuccessful analysis runs and experiments to be stored in a history. 
    # Stages for unsuccessful: "Error", "Failed", "Inconclusive"
    # Defaults to 5.
    unsuccessfulRunHistoryLimit: 10

  # Label selector for pods. Existing ReplicaSets whose pods are selected by
  # this will be the ones affected by this rollout. It must match the pod
  # template's labels.
  selector:
    matchLabels:
      app: guestbook

  # WorkloadRef holds a references to a workload that provides Pod template 
  # (e.g. Deployment). If used, then do not use Rollout template property.
  workloadRef: 
    apiVersion: apps/v1
    kind: Deployment
    name: rollout-ref-deployment

  # Template describes the pods that will be created. Same as deployment.
  # If used, then do not use Rollout workloadRef property. 
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

  # Whether to abort the update when ProgressDeadlineSeconds is exceeded.
  # Optional and default is false.
  progressDeadlineAbort: false

  # UTC timestamp in which a Rollout should sequentially restart all of
  # its pods. Used by the `kubectl argo rollouts restart ROLLOUT` command.
  # The controller will ensure all pods have a creationTimestamp greater
  # than or equal to this value.
  restartAt: "2020-03-30T21:19:35Z"

  # The rollback window provides a way to fast track deployments to
  # previously deployed versions.
  # Optional, and by default is not set.
  rollbackWindow:
    revisions: 3

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

      # Add a delay in second before scaling down the preview replicaset
      # if update is aborted. 0 means not to scale down. Default is 30 second
      abortScaleDownDelaySeconds: 30

      # Anti Affinity configuration between desired and previous ReplicaSet.
      # Only one must be specified
      antiAffinity:
        requiredDuringSchedulingIgnoredDuringExecution: {}
        preferredDuringSchedulingIgnoredDuringExecution:
          weight: 1 # Between 1 - 100
          
      # activeMetadata will be merged and updated in-place into the ReplicaSet's spec.template.metadata
      # of the active pods. +optional
      activeMetadata:
        labels:
          role: active
          
      # Metadata which will be attached to the preview pods only during their preview phase.
      # +optional
      previewMetadata:
        labels:
          role: preview

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

      # Adds a delay before scaling down the previous ReplicaSet when the
      # canary strategy is used with traffic routing (default 30 seconds).
      # A delay in scaling down the previous ReplicaSet is needed after
      # switching the stable service selector to point to the new ReplicaSet,
      # in order to give time for traffic providers to re-target the new pods.
      # This value is ignored with basic, replica-weighted canary without
      # traffic routing.
      scaleDownDelaySeconds: 30

      # The minimum number of pods that will be requested for each ReplicaSet
      # when using traffic routed canary. This is to ensure high availability
      # of each ReplicaSet. Defaults to 1. +optional
      minPodsPerReplicaSet: 2

      # Limits the number of old RS that can run at one time before getting
      # scaled down. Defaults to nil
      scaleDownDelayRevisionLimit: 2

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

      # set canary scale to a explicit count without changing traffic weight
      # (supported only with trafficRouting)
      - setCanaryScale:
          replicas: 3

      # set canary scale to a percentage of spec.replicas without changing traffic weight
      # (supported only with trafficRouting)
      - setCanaryScale:
          weight: 25

      # set canary scale to match the canary traffic weight (default behavior)
      - setCanaryScale:
          matchTrafficWeight: true

      # Sets header based route with specified header values
      # Setting header based route will send all traffic to the canary for the requests 
      # with a specified header, in this case request header "version":"2"
      # (supported only with trafficRouting, for Istio only at the moment)
      - setHeaderRoute:
          # Name of the route that will be created by argo rollouts this must also be configured
          # in spec.strategy.canary.trafficRouting.managedRoutes
          name: "header-route-1"
          # The matching rules for the header route, if this is missing it acts as a removal of the route.
          match:
              # headerName The name of the header to apply the match rules to.
            - headerName: "version"
              # headerValue must contain exactly one field of exact, regex, or prefix. Not all traffic routers support 
              # all types
              headerValue:
                # Exact will only match if the header value is exactly the same
                exact: "2"
                # Will match the rule if the regular expression matches
                regex: "2.0.(.*)"
                # prefix will be a prefix match of the header value
                prefix: "2.0"
                
        # Sets up a mirror/shadow based route with the specified match rules
        # The traffic will be mirrored at the configured percentage to the canary service
        # during the rollout
        # (supported only with trafficRouting, for Istio only at the moment)
      - setMirrorRoute:
          # Name of the route that will be created by argo rollouts this must also be configured
          # in spec.strategy.canary.trafficRouting.managedRoutes
          name: "header-route-1"
          # The percentage of the matched traffic to mirror to the canary
          percentage: 100
          # The matching rules for the header route, if this is missing it acts as a removal of the route.
          # All conditions inside a single match block have AND semantics, while the list of match blocks have OR semantics.
          # Each type within a match (method, path, headers) must have one and only one match type (exact, regex, prefix)
          # Not all match types (exact, regex, prefix) will be supported by all traffic routers.
          match:
            - method: # What HTTP method to match
                exact: "GET"
                regex: "P.*"
                prefix: "POST"
              path: # What HTTP url paths to match.
                exact: "/test"
                regex: "/test/.*"
                prefix: "/"
              headers:
                agent-1b: # What HTTP header name to use in the match.
                  exact: "firefox"
                  regex: "firefox2(.*)"
                  prefix: "firefox"

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
            # optional, creates a service for the experiment if set
            service:
              # optional, service: {} is also acceptable if name is not included
              name: test-service
          - name: canary
            specRef: canary
            # optional, set the weight of traffic routed to this version
            weight: 10
          analyses:
          - name : mann-whitney
            templateName: mann-whitney
            # Metadata which will be attached to the AnalysisRun.
            analysisRunMetadata:
              labels:
                app.service.io/analysisType: smoke-test
              annotations:
                link.argocd.argoproj.io/external-link: http://my-loggin-platform.com/pre-generated-link

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
        # This is a list of routes that Argo Rollouts has the rights to manage it is currently only required for
        # setMirrorRoute and setHeaderRoute. The order of managedRoutes array also sets the precedence of the route
        # in the traffic router. Argo Rollouts will place these routes in the order specified above any routes already
        # defined in the used traffic router if something exists. The names here must match the names from the 
        # setHeaderRoute and setMirrorRoute steps.
        managedRoutes:
          - name: set-header
          - name: mirror-route
        # Istio traffic routing configuration
        istio:
          # Either virtualService or virtualServices can be configured.
          virtualService: 
            name: rollout-vsvc  # required
            routes:
            - primary # optional if there is a single route in VirtualService, required otherwise
          virtualServices:
          # One or more virtualServices can be configured
          - name: rollouts-vsvc1  # required
            routes:
              - primary # optional if there is a single route in VirtualService, required otherwise
          - name: rollouts-vsvc2  # required
            routes:
              - secondary # optional if there is a single route in VirtualService, required otherwise

        # NGINX Ingress Controller routing configuration
        nginx:
          # Either stableIngress or stableIngresses must be configured, but not both.
          stableIngress: primary-ingress
          stableIngresses:
            - primary-ingress
            - secondary-ingress
            - tertiary-ingress
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

      # Add a delay in second before scaling down the canary pods when update
      # is aborted for canary strategy with traffic routing (not applicable for basic canary).
      # 0 means canary pods are not scaled down. Default is 30 seconds.
      abortScaleDownDelaySeconds: 30

status:
  pauseConditions:
  - reason: StepPause
    startTime: 2019-10-00T1234
  - reason: BlueGreenPause
    startTime: 2019-10-00T1234
  - reason: AnalysisRunInconclusive
    startTime: 2019-10-00T1234 
```
## Examples

You can find examples of Rollouts at:

 * The [example directory](https://github.com/argoproj/argo-rollouts/tree/master/examples)
 * The [Argo Rollouts Demo application](https://github.com/argoproj/rollouts-demo)
