# Experiment CRD

## What is the Experiment CRD?
The Experiment CRD allows users to run multiple PodSpecs for a specified duration. The experiment controller monitors these ReplicaSets until the duration listed in the Experiment has passed. At that point, the Experiment's ReplicaSets are scaled-down and marked as completed.

## Experiment Spec
Below is an example of an experiment that creates two ReplicaSets with 1 replica each and runs them for 60 seconds once they are both marked as available.

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Experiment
metadata:
  name: example-experiment
spec:
  duration: 60 # How long to run the Experiment once the ReplicaSets created from the templates are healthy
  progressDeadlineSeconds: 30
  templates:
  - replicas: 1
    name: baseline # (required) Unique name for the template that gets used as a part of the ReplicaSet name.
    minReadySeconds: 10
    selector: # Same selector that has been as in Deployments and Rollouts
      matchLabels:
        app: guestbook
        version: v1
    template: # Same Pod Spec that has been as in Deployments and Rollouts
      metadata:
        labels:
          app: guestbook
      spec:
        containers:
        - name: guestbook
          image: gcr.io/heptio-images/ks-guestbook-demo:0.1
          ports:
          - containerPort: 80
  - replicas: 1
    name: canary
    minReadySeconds: 10
    selector: # Same selector that has been as in Deployments and Rollouts
      matchLabels:
        app: guestbook
        version: v1
    template: # Same Pod Spec that has been as in Deployments and Rollouts
      metadata:
        labels:
          app: guestbook
          version: v2
      spec:
        containers:
        - name: guestbook
          image: gcr.io/heptio-images/ks-guestbook-demo:0.2
          ports:
          - containerPort: 80
```

## How does it work?
The Experiment controller manages the creation and deletion of ReplicaSets based on the Experiment CR. Each template listed in the `spec.templates` field of the Experiment creates its ReplicaSets based off of the PodSpec within the template. After the Experiment creation, the controller creates a ReplicaSets for each template listed in the `spec.templates`. The ReplicaSet's names are generated from the combination of the Experiment's name, template's name, and a hash generated from that template's PodSpec template with a collision counter.

Once the controller creates the ReplicaSets, the Experiment is running but not available. The controller monitors the ReplicaSets and Experiment until the controller detects the ReplicaSet as available or the Progress Deadline Seconds has passed. The Progress Deadline Seconds listed at `spec.progressDeadlineSeconds` defines the amount of time the ReplicaSets has to make progress to the available state. If none of the ReplicaSets make any progress and the deadline passes, the Experiment enters a Degraded state. It is considered a failure, and the existing ReplicaSets are scaled-down. On the other hand, once the controller sees that all the ReplicaSets are available, the Experiment is considered available. The Experiment stays in this state for the duration listed in the `spec.duration` field. Once that duration passes, the Experiment scales down the running ReplicaSets and mark itself as Completed. After this point, the controller takes no other actions on that Experiment.


## Use cases of the Experiment CRD

- A user wants to run two versions of an application for a specific duration to enable Kayenta-style analysis of their application. The Experiment CRD creates 2 ReplicaSets (a baseline and a canary) based on the `spec.templates` field of the Experiment and waits until both are healthy. After the duration passes, the Experiment scales down the ReplicaSets, and the user can start the Kayenta analysis run.

- A user can use experiments to enable A/B/C testing by launching multiple experiments with a different version of their application for a long duration. Each Experiment has one PodSpec template that defines a specific version a user would want to run. The Experiment allows users to launch multiple experiments at once and keep each Experiment self-contained.
