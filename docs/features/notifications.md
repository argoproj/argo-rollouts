# Notifications

!!! important
    Available since v1.1

Argo Rollouts provides notifications powered by the [Notifications Engine](https://github.com/argoproj/notifications-engine).
Controller administrators can leverage flexible systems of triggers and templates to configure notifications requested
by the end users. The end-users can subscribe to the configured triggers by adding an annotation to the Rollout objects.

## Configuration

The trigger defines the condition when the notification should be sent as well as the notification content template.
By default, Argo Rollouts comes with a list of built-in triggers that cover the most important events of Argo Rollout life-cycle.
Both triggers and templates are configured in the `argo-rollouts-notification-configmap` ConfigMap. In order to get
started quickly, you can use pre-configured notification templates defined in [notifications-install.yaml](https://github.com/argoproj/argo-rollouts/blob/master/manifests/notifications-install.yaml).

If you are leveraging Kustomize it is recommended to include [notifications-install.yaml](https://github.com/argoproj/argo-rollouts/blob/master/manifests/notifications-install.yaml) as a remote
resource into your `kustomization.yaml` file:

```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

resources:
- https://github.com/argoproj/argo-rollouts/releases/latest/download/install.yaml
- https://github.com/argoproj/argo-rollouts/releases/latest/download/notifications-install.yaml
```

After including the `argo-rollouts-notification-configmap` ConfigMap the administrator needs to configure integration
with the required notifications service such as Slack or MS Teams. An example below demonstrates Slack integration:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: argo-rollouts-notification-configmap
data:
  # detail of the templates is omitted
  # detail of the triggers is omitted
  service.slack: |
    token: $slack-token
---
apiVersion: v1
kind: Secret
metadata:
  name: argo-rollouts-notification-secret
stringData:
  slack-token: <my-slack-token>
```

Learn more about supported services and configuration settings in services [documentation](../generated/notification-services/overview.md).

## Namespace based configuration

!!! important
Available since v1.6

A common installation method for Argo Rollouts is to install it in a dedicated namespace to manage a whole cluster. In this case, the administrator is the only
person who can configure notifications in that namespace generally. However, in some cases, it is required to allow end-users to configure notifications
for their Rollout resources. For example, the end-user can configure notifications for their Rollouts in the namespace where they have access to and their rollout is running in.

To use this feature all you need to do is create the same configmap named `argo-rollouts-notification-configmap` and possibly 
a secret `argo-rollouts-notification-secret` in the namespace where the rollout object lives. When it is configured this way the controller
will send notifications using both the controller level configuration (the configmap located in the same namespaces as the controller) as well as 
the configmap located in the same namespaces where the rollout object is at.

To enable you need to add a flag to the controller `--self-service-notification-enabled`

## Default Trigger templates

Currently, the following triggers have [built-in templates](https://github.com/argoproj/argo-rollouts/tree/master/manifests/notifications).

* `on-analysis-run-error` when an error occurs during the execution of an analysis run
* `on-analysis-run-failed` when an analysis run fails
* `on-analysis-run-running` when an analysis run is running
* `on-rollout-aborted` when a rollout process is aborted before completion.
* `on-rollout-completed` when a rollout is finished and all its steps are completed
* `on-rollout-paused` when a rollout is paused
* `on-rollout-step-completed` when an individual step inside a rollout definition is completed
* `on-rollout-updated` when a rollout definition is changed
* `on-scaling-replica-set` when the number of replicas in a rollout is changed

## Subscriptions

The end-users can start leveraging notifications using `notifications.argoproj.io/subscribe.<trigger>.<service>: <recipient>` annotation.
For example, the following annotation subscribes two Slack channels to notifications about canary rollout step completion:

```yaml
---
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: rollout-canary
  annotations:
    notifications.argoproj.io/subscribe.on-rollout-step-completed.slack: my-channel1;my-channel2

```

Annotation key consists of following parts:

* `on-rollout-step-completed` - trigger name
* `slack` - notification service name
* `my-channel1;my-channel2` - a semicolon separated list of recipients

## Customization

The Rollout administrator can customize the notifications by configuring notification templates and custom triggers
in `argo-rollouts-notification-configmap` ConfigMap.

### Templates

The notification template is a stateless function that generates the notification content. The template is leveraging
[html/template](https://golang.org/pkg/html/template/) golang package. It is meant to be reusable and can be referenced by multiple triggers.

An example below demonstrates a sample template:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: argo-rollouts-notification-configmap
data:
  template.my-purple-template: |
    message: |
      Rollout {{.rollout.metadata.name}} has purple image
    slack:
        attachments: |
            [{
              "title": "{{ .rollout.metadata.name}}",
              "color": "#800080"
            }]
```

Each template has access to the following fields:

- `rollout` holds the rollout object.
- `recipient` holds the recipient name.
- `context` holds user-defined context from the notifications ConfigMap.

The `message` field of the template definition allows creating a basic notification for any notification service. You can
leverage notification service-specific fields to create complex notifications. For example using service-specific you can
add blocks and attachments for Slack, subject for Email or URL path, and body for Webhook. See corresponding service
[documentation](../generated/notification-services/overview.md) for more information.

### Custom Triggers

In addition to custom notification template administrator and configure custom triggers. Custom trigger defines the
condition when the notification should be sent. The definition includes name, condition and notification templates reference.
The condition is a predicate expression that returns true if the notification should be sent. The trigger condition
evaluation is powered by [expr-lang/expr](https://github.com/expr-lang/expr).
The condition language syntax is described at [Language-Definition.md](https://github.com/expr-lang/expr/blob/master/docs/language-definition.md).

The trigger is configured in `argo-rollouts-notification-configmap` ConfigMap. For example the following trigger sends a notification
when rollout pod spec uses `argoproj/rollouts-demo:purple` image:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: argo-rollouts-notification-configmap
data:
  trigger.on-purple: |
    - send: [my-purple-template]
      when: rollout.spec.template.spec.containers[0].image == 'argoproj/rollouts-demo:purple'
```

Each condition might use several templates. Typically each template is responsible for generating a service-specific notification part.

### User-Defined Context

You can define shared context for notification templates by setting `context` in the `argo-rollouts-notification-configmap`
ConfigMap. The context is parsed as a flat YAML map of string values and exposed to templates as `.context`. Scalars such
as numbers and booleans are kept as their text. Nested values are not supported, and a `context` value that is not valid
YAML, or not a flat map, makes the notification configuration fail to load.

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: argo-rollouts-notification-configmap
data:
  context: |
    region: east
    environmentName: staging

  template.rollout-completed: |
    message: "Rollout {{ .rollout.metadata.name }} completed in {{ .context.environmentName }} in the {{ .context.region }} region."
```

### Notification Metrics

The following prometheus metrics are emitted when notifications are enabled in argo-rollouts.

- `notification_send_success` is a counter that measures how many times the notification is sent successfully.
- `notification_send_error` is a counter that measures how many times the notification failed to send.
- `notification_send` is a histogram that measures performance of sending notification.

## RolloutPlugin
<!-- TODOH update the trigger names to have on-rollout-plugin-->
[RolloutPlugin](rolloutplugin/index.md) resources use the same `argo-rollouts-notification-configmap`, secret,
services and subscription annotations as Rollouts. Namespace based configuration works the same way when
`--self-service-notification-enabled` is set.

There are no built-in templates or triggers for RolloutPlugin, so you need to define your own. The following triggers
fire when the RolloutPlugin controller records the matching event:

* `on-rollout-plugin-progressing` when a rollout starts for a new revision
* `on-rollout-plugin-paused` when a rollout is paused, at a pause step or manually
* `on-rollout-plugin-resumed` when a pause step completes
* `on-rollout-plugin-completed` when a rollout is finished and every pod runs the new revision
* `on-rollout-plugin-aborted` when an abort has rolled every pod back to the stable revision
* `on-abort-ignored` when an abort is ignored because every pod already runs the new revision
* `on-progress-deadline-exceeded` when a rollout times out
* `on-invalid-spec` when the RolloutPlugin spec becomes invalid
* `on-reconciliation-error` when an AnalysisRun can't be created or updated
* `on-promote-full-no-op` when promote full is requested with no rollout in progress
* `on-analysis-run-<phase>` when an AnalysisRun changes phase, for example `on-analysis-run-failed`
* `on-deletion-cleanup-failed` when the plugin fails to release the workload while the RolloutPlugin is deleted
* `on-rollout-plugin-deleted` when a RolloutPlugin is deleted

Each template has access to the following fields:

- `rolloutPlugin` holds the RolloutPlugin object.
- `analysisRuns` holds the AnalysisRuns owned by the RolloutPlugin, newest first.
- `time` holds the `Parse` and `Now` time functions.
- `secrets` holds the data of the `argo-rollouts-notification-secret` secret.
- `context` holds the [user-defined context](#user-defined-context) from the notifications ConfigMap.

For example:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: argo-rollouts-notification-configmap
data:
  template.rollout-plugin-aborted: |
    message: |
      RolloutPlugin {{.rolloutPlugin.metadata.name}} was aborted: {{.rolloutPlugin.status.message}}
  trigger.on-rollout-plugin-aborted: |
    - send: [rollout-plugin-aborted]
---
apiVersion: argoproj.io/v1alpha1
kind: RolloutPlugin
metadata:
  name: web-rollout
  annotations:
    notifications.argoproj.io/subscribe.on-rollout-plugin-aborted.slack: my-channel
```

[Custom triggers](#custom-triggers) with a `when` condition work as well, using the `rolloutPlugin` variable:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: argo-rollouts-notification-configmap
data:
  trigger.on-rollout-plugin-degraded: |
    - send: [rollout-plugin-aborted]
      when: rolloutPlugin.status.phase == 'Degraded'
```
