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
for 60 seconds once they both become available. Also, the controller launches two AnalysisRuns after
the ReplicaSets become available. 

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Experiment
metadata:
  name: example-experiment
spec:
  duration: 1m # How long to run the Experiment once the ReplicaSets created from the templates are healthy
  progressDeadlineSeconds: 30
  templates:
  - name: purple # (required) Unique name for the template that gets used as a part of the ReplicaSet name.
    replicas: 1
    selector: # Same selector that has been as in Deployments and Rollouts
      matchLabels:
        app: canary-demo
        color: purple
    template:
      metadata:
        labels:
          app: canary-demo
          color: purple
      spec: # Same Pod Spec that has been as in Deployments and Rollouts
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
    selector: # Same selector that has been as in Deployments and Rollouts
      matchLabels:
        app: canary-demo
        color: orange
    template:
      metadata:
        labels:
          app: canary-demo
          color: orange
      spec: # Same Pod Spec that has been as in Deployments and Rollouts
        containers:
        - name: rollouts-demo
          image: argoproj/rollouts-demo:orange
          imagePullPolicy: Always
          ports:
          - name: http
            containerPort: 8080
            protocol: TCP
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
```

## How does it work?

The Experiment controller has two primary responsibilities for each Experiment:

1. Creating and scaling ReplicaSets
1. Creating and watching AnalysisRuns

The controller creates a ReplicaSet for each template in the Experiment's `.spec.templates`. Each 
template needs a unique name as the controller generates the ReplicaSet's names from the combination
of the Experiment's name and template's name. Once the controller creates the ReplicaSets, it waits
until those new ReplicaSets become available. Once all the ReplicaSets are available, the controller
marks the Experiment as running. The Experiment stays in this state for the duration listed in the
`spec.duration` field or indefinitely if omitted. 

Once the Experiment is running, the controller creates AnalysisRuns for each analysis listed in the
Experiment's `.spec.analysis` field. These AnalysisRun execute in parallel with the running
ReplicaSets. The controller generates the AnalysisRun's name by combining the experiment name and
the analysis name with a dash. If an AnalysisRun exists with that name, the controller appends a
number to the generated name before recreating the AnalysisRun. If there is another collision, the
controller increments the number and try again until it creates an AnalysisRun. Once the Experiment
finishes, the controller scales down the ReplicaSets it created and terminates the AnalysisRuns if
they have not finished.

An Experiment is considered complete when:

1. More than the `spec.Duration` amount of time has passed since the ReplicaSets became healthy.
1. One of the ReplicaSets does not become available, and the progress deadline seconds pass.
1. An AnalysisRun created by an Experiment enters a failed or error state.
1. An external process (i.e. user or pipeline) sets the `.spec.terminate` to true

## Run Experiment Indefinitely

Experiments can run for an indefinite duration by omitting the duration field. Indefinite
experiments would be stopped externally, or through the completion of a referenced analysis.


## Integration With Rollouts

A rollout using the Canary strategy can create an experiment using the `experiment` step. The
experiment step serves as a blocking step for the Rollout as the Rollout does not continue until the
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

Additionally, the Experiment will perform analysis using the `mann-whitney` AnalysisTemplate. That
AnalysisTemplate is supplied with the pod-hash details of the baseline and canary to perform the
necessary metrics queries, using the `{{templates.baseline.podTemplateHash}}` and
`{{templates.canary.podTemplateHash}}` variables respectively.


!!! note 
    The pod-hashes of the `baseline`/`canary` ReplicaSets created by the Experiment, will have
    different values than the pod-hashes of the `stable`/`canary` ReplicaSets created by the
    Rollout. This is despite the fact that the PodSpec are the same. This is intentional behavior,
    in order to allow the metrics of the Experiment's pods to be delineated and queried separately
    from the metrics of the Rollout pods.
