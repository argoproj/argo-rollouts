# Canary Step Plugins

!!! warning "Alpha Feature (Since 1.8.0)"

    This is an experimental, [alpha-quality](https://github.com/argoproj/argoproj/blob/main/community/feature-status.md#alpha)
    feature that allows you to execute plugins during canary steps.

Argo Rollouts supports getting step plugins via 3rd party [plugin system](../../plugins.md). This allows users to extend the capabilities of Rollouts
to support executing arbitrary steps during the canary. Rollout's uses a plugin library called
[go-plugin](https://github.com/hashicorp/go-plugin) to do this.

## Installing

There are two methods of installing and using an Argo Rollouts plugin. The first method is to mount up the plugin executable
into the rollouts controller container. The second method is to use a HTTP(S) server to host the plugin executable.

### Mounting the plugin executable into the rollouts controller container

There are a few ways to mount the plugin executable into the rollouts controller container. Some of these will depend on your
particular infrastructure. Here are a few methods:

- Using an init container to download the plugin executable
- Using a Kubernetes volume mount with a shared volume such as NFS, EBS, etc.
- Building the plugin into the rollouts controller container

Then you can use the ConfigMap to point to the plugin executable file location. Example:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: argo-rollouts-config
data:
  stepPlugins: |-
    - name: "argoproj-labs/sample-step" # name of the plugin, it must match the name required by the plugin so it can find it's configuration
      location: "file://./my-custom-plugin" # supports http(s):// urls and file://
```

### Using a HTTP(S) server to host the plugin executable

!!! warning "Installing a plugin with http(s)"

    Depending on which method you use to install and the plugin, there are some things to be aware of.
    The rollouts controller will not start if it can not download or find the plugin executable. This means that if you are using
    a method of installation that requires a download of the plugin and the server hosting the plugin for some reason is not available and the rollouts
    controllers pod got deleted while the server was down or is coming up for the first time, it will not be able to start until
    the server hosting the plugin is available again.

    Argo Rollouts will download the plugin at startup only once but if the pod is deleted it will need to download the plugin again on next startup. Running
    Argo Rollouts in HA mode can help a little with this situation because each pod will download the plugin at startup. So if a single pod gets
    deleted during a server outage, the other pods will still be able to take over because there will already be a plugin executable available to it. It is the
    responsibility of the Argo Rollouts administrator to define the plugin installation method considering the risks of each approach.

Argo Rollouts supports downloading the plugin executable from a HTTP(S) server. To use this method, you will need to
configure the controller via the `argo-rollouts-config` ConfigMap and set `pluginLocation` to a http(s) url. Example:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: argo-rollouts-config
data:
  stepPlugins: |-
    - name: "argoproj-labs/sample-nginx" # name of the plugin, it must match the name required by the plugin so it can find it's configuration
      location: "https://github.com/argoproj-labs/rollouts-plugin-trafficrouter-sample-nginx/releases/download/v0.0.1/metric-plugin-linux-amd64" # supports http(s):// urls and file://
      sha256: "08f588b1c799a37bbe8d0fc74cc1b1492dd70b2c" # optional sha256 checksum of the plugin executable
```

### Disabling a plugin

A step plugin that will execute during your Rollouts will fail the canary deployment whenever there is an unhandled error.
If a step plugin is used in multiple rollouts and is suddenly unstable, none of the rollouts will be able to progress.
To make the plugin less disruptive and the upgrades easier, you can use the `disabled` flag in the plugin configuration to
disable it globally. This will skip the plugin execution in every Rollout where it is configured, and progress to the next canary step.

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: argo-rollouts-config
data:
  stepPlugins: |-
    - name: "argoproj-labs/sample-nginx"
      location: "https://github.com/argoproj-labs/rollouts-plugin-trafficrouter-sample-nginx/releases/download/v0.0.1/metric-plugin-linux-amd64"
      disabled: true # Skip all canary steps using this plugin because it may be faulty.
```

## Usage

You can execute a configured step plugin at any point during your canary steps.
The plugin will be executed and the rollout step will be progressing until the plugin execution returns a status of
`Successful`, `Failed` or `Error`. Once completed, the rollout will progress to the next configured step.

For the available plugin `config`, refer to each specific plugin documentation.

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: example-plugin-ro
spec:
  strategy:
    canary:
      steps:
        - plugin:
            name: argoproj-labs/step-exec
            config:
              command: echo "hello world"
```

### Plugin Statuses

To know the result of your plugin, you can use the `status.stepPluginStatuses[]` property to find the status that correspond to
your execution. Each status item is unique by `index`, `name` and `operation`. The `operation` can be one of the following:

- `Run`: The main operation that execute the plugin.
- `Terminate`: The operation called on your plugin when the `Run` operation is still ongoing, but your rollout is aborted.
- `Abort`: The operation called on your plugin when it is aborted. This will be called for every `Successful` `Run` operation.

## Implementation

As a plugin developer, your step plugin should follow some conventions to make it predictable and easier to use.

### Run operation

The run operation is the method called on your plugin when executed. The operation can be called
**multiple times**. It is the responsibility of the plugin's implementation to validate if the desired
plugin actions were already taken or not.

#### Long-running operations

If the plugin needs to execute an operation that may take a long time, or poll for a result, it can return
early with a `Running` phase and a `RequeueAfter` duration. The controller will requeue the rollout and call the `Run` operation
again after the `RequeueAfter` has expired. The `Status` property on the return object can hold any information that would be
necessary to retrieve the state in subsequent executions.

### Terminate operation

If the `Run` operation returns with a `Running` phase and the rollout needs to cancel the execution, the controller will call the plugin's terminate method
with the state of the ongoing `Running` operation. The plugin can use this method to cancel any ongoing information.
This is often called if the rollout is fully promoted during a plugin execution.

If the terminate operation has an error and fails, it will not be retried. The plugin should have a mechanism to cancel
suspiciously long-running operations if necessary.

### Abort operation

The abort operation will be called whenever a rollout is aborted and plugin step `Run` operation was `Successful` or currently `Running`.
The operation will be called in the reverse execution order with the existing state of the operation it is aborting.

If the abort operation has an error and fails, it will not be retried. The plugin should have a mechanism to cancel
suspiciously long-running operations if necessary.

### Returning errors

The plugin can return an error for unhandled operations. In that case, the rollout will handle that error and apply a
backoff mechanism to retry the execution of the plugin until it returns `Successful` or `Failed` phase. When an error happens, the
`Status` returned by the plugin is not persisted, allowing it to retry later on the last known valid status.

The controller will keep retrying until it succeeds, or the rollout is aborted.

## List of Available Plugins (alphabetical order)

If you have created a plugin, please submit a PR to add it to this list.

### [plugin-name](#plugin-name)

- Brief plugin description
