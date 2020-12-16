# Experiment CRD

## What is the Experiment CRD?

The Experiment CRD allows users to have ephemeral runs of one or more ReplicaSets. In addition to
running ephemeral ReplicaSets, the Experiment CRD can launch AnalysisRuns alongside the ReplicaSets.
Generally, those AnalysisRun is used to confirm that new ReplicaSets are running as expected.

## Use cases of Experiments

- A user wants to run two versions of an application for a specific duration to enable Kayenta-style
analysis of their application. The Experiment CRD creates 2 ReplicaSets (a baseline and a canary)
based on the `spec.templates` field of the Experiment and waits until both are healthy. After the
duration passes, the Experiment scales down the ReplicaSets, and the user can start the Kayenta
analysis run.

- A user can use experiments to enable A/B/C testing by launching multiple experiments with a
different version of their application for a long duration. Each Experiment has one PodSpec template
that defines a specific version a user would want to run. The Experiment allows users to launch
multiple experiments at once and keep each Experiment self-contained.

- Launching a new version of an existing application with different labels to avoid receiving
traffic from a Kubernetes service. The user can run tests against the new version before continuing
the Rollout.

## Experiment Spec

Below is an example of an experiment that creates two ReplicaSets with 1 replica each and runs them
for 20 minutes once they both become available. Additionally, several AnalysisRuns are run to
perform analysis against the pods of the Experiment 

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Experiment
metadata:
  name: example-experiment
spec:
  # Duration of the experiment, beginning from when all ReplicaSets became healthy (optional)
  # If omitted, will run indefinitely until terminated, or until all analyses which were marked
  # `requiredForCompletion` have completed.
  duration: 20m

  # Deadline in seconds in which a ReplicaSet should make progress towards becoming available.
  # If exceeded, the Experiment will fail.
  progressDeadlineSeconds: 30

  # List of pod template specs to run in the experiment as ReplicaSets
  templates:
  - name: purple
    # Number of replicas to run (optional). If omitted, will run a single replica
    replicas: 1
    selector:
      matchLabels:
        app: canary-demo
        color: purple
    template:
      metadata:
        labels:
          app: canary-demo
          color: purple
      spec:
        containers:
        - name: rollouts-demo
          image: argoproj/rollouts-demo:purple
          imagePullPolicy: Always
          ports:
          - name: http
            containerPort: 8080
            protocol: TCP
  - name: orange
    replicas: 1
    minReadySeconds: 10
    selector:
      matchLabels:
        app: canary-demo
        color: orange
    template:
      metadata:
        labels:
          app: canary-demo
          color: orange
      spec:
        containers:
        - name: rollouts-demo
          image: argoproj/rollouts-demo:orange
          imagePullPolicy: Always
          ports:
          - name: http
            containerPort: 8080
            protocol: TCP

  # List of AnalysisTemplate references to perform during the experiment
  analyses:
  - name: purple
    templateName: http-benchmark
    args:
    - name: host
      value: purple
  - name: orange
    templateName: http-benchmark
    args:
    - name: host
      value: orange
  - name: compare-results
    templateName: compare
    # If requiredForCompletion is true for an analysis reference, the Experiment will not complete
    # until this analysis has completed.
    requiredForCompletion: true
    args:
    - name: host
      value: purple

```

## Experiment Lifecycle

An Experiment is intended to temporarily run one or more templates. The lifecycle of an Experiment
is as follows:

1. Create and scale a ReplicaSet for each pod template specified under `spec.templates`
2. Wait for all ReplicaSets reach full availability. If a ReplicaSet does not become available
   within `spec.progressDeadlineSeconds`, the Experiment will fail. Once available, the Experiment
   will transition from the `Pending` state to a `Running` state.
3. Once an Experiment is considered `Running`, it will begin an AnalysisRun for every
   AnalysisTemplate referenced under `spec.analyses`.
4. If a duration is specified under `spec.duration`, the Experiment will wait until the duration
   has elapsed before completing the Experiment.
5. If an AnalysisRun fails or errors, the Experiment will end prematurely, with a status equal to
   the unsuccessful AnalysisRun (i.e. `Failed` or `Error`)
6. If one or more of the referenced AnalysisTemplates is marked with `requiredForCompletion: true`,
   the Experiment will not complete until those AnalysisRuns have completed, even if it exceeds
   the Experiment duration.
7. If neither a `spec.duration` or `requiredForCompletion: true` is specified, the Experiment will
   run indefinitely, until explicitly terminated (by setting `spec.terminate: true`).
8. Once an Experiment is complete, the ReplicaSets will be scaled to zero, and any incomplete
   AnalysisRuns will be terminated.

!!! note
    ReplicaSet names are generated by combining the Experiment name with the template name.

## Integration With Rollouts

A rollout using the Canary strategy can create an experiment using an `experiment` step. The
experiment step serves as a blocking step for the Rollout, and a Rollout will not continue until the
Experiment succeeds. The Rollout creates an Experiment using the configuration in the experiment
step of the Rollout. If the Experiment fails or errors, the Rollout will abort.

!!! note
    Experiment names are generated by combining the Rollout's name, the PodHash of
    the new ReplicaSet, the current revision of the Rollout, and the current step-index.

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: guestbook
  labels:
    app: guestbook
spec:
...
  strategy:
    canary: 
      steps:
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
            args:
            - name: baseline-hash
              value: "{{templates.baseline.podTemplateHash}}"
            - name: canary-hash
              value: "{{templates.canary.podTemplateHash}}"
```

In the example above, during an update of a Rollout, the Rollout will launch an Experiment. The
Experiment will create two ReplicaSets: `baseline` and `canary`, with one replica each, and will run
for one hour. The `baseline` template uses the PodSpec from the stable ReplicaSet, and the canary
template uses the PodSpec from the canary ReplicaSet.

Additionally, the Experiment will perform analysis using the AnalysisTemplate named `mann-whitney`.
The AnalysisRun is supplied with the pod-hash details of the baseline and canary to perform the
necessary metrics queries, using the `{{templates.baseline.podTemplateHash}}` and
`{{templates.canary.podTemplateHash}}` variables respectively.

!!! note 
    The pod-hashes of the `baseline`/`canary` ReplicaSets created by the Experiment, will have
    different values than the pod-hashes of the `stable`/`canary` ReplicaSets created by the
    Rollout. This is despite the fact that the PodSpec are the same. This is intentional behavior,
    in order to allow the metrics of the Experiment's pods to be delineated and queried separately
    from the metrics of the Rollout pods.
