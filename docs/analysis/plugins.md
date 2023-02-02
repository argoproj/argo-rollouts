# Metric Plugins

!!! important Available since v1.5

Argo Rollouts supports getting analysis metrics via 3rd party plugin system. This allows users to extend the capabilities of Rollouts 
to support metric providers that are not natively supported. Rollout's uses a plugin library called
[go-plugin](https://github.com/hashicorp/go-plugin) to do this. You can find a sample plugin 
here: [sample-rollouts-metric-plugin](https://github.com/argoproj-labs/sample-rollouts-metric-plugin)

## Using a Metric Plugin

There are two methods of installing and using an argo rollouts plugin. The first method is to mount up the plugin executable
into the rollouts controller container. The second method is to use a HTTP(S) server to host the plugin executable.

### Mounting the plugin executable into the rollouts controller container

To use this method, you will need to build or download the plugin executable and then mount it into the rollouts controller container.
The plugin executable must be mounted into the rollouts controller container at the path specified by the `--metric-plugin-location` flag.

There are a few ways to mount the plugin executable into the rollouts controller container. Some of these will depend on your
particular infrastructure. Here are a few methods:

* Using an init container to download the plugin executable
* Using a Kubernetes volume mount with a shared volume such as NFS, EBS, etc.
* Building the plugin into the rollouts controller container

Then you can use the configmap to point to the plugin executable. Example:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: argo-rollouts-config
data:
  plugins: |-
    metrics:
    - name: "prometheus" # name the plugin uses to find this configuration, it must match the name required by the plugin
      pluginLocation: "file://./my-custom-plugin" # supports http(s):// urls and file://
```

### Using a HTTP(S) server to host the plugin executable

Argo Rollouts supports downloading the plugin executable from a HTTP(S) server. To use this method, you will need to 
configure the controller via the `argo-rollouts-config` configmap and set `pluginLocation` to a http(s) url. Example:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: argo-rollouts-config
data:
  plugins: |-
    metrics:
    - name: "prometheus" # name the plugin uses to find this configuration, it must match the name required by the plugin
      pluginLocation: "https://github.com/argoproj-labs/sample-rollouts-metric-plugin/releases/download/v0.0.3/metric-plugin-linux-amd64" # supports http(s):// urls and file://
      pluginSha256: "08f588b1c799a37bbe8d0fc74cc1b1492dd70b2c" #optional sha256 checksum of the plugin executable
```

## Some words of caution

Depending on which method you use to install and the plugin, there are some things to be aware of.
The rollouts controller will not start if it can not download or find the plugin executable. This means that if you are using
a method of installation that requires a download of the plugin and the server hosting the plugin for some reason is not available and the rollouts
controllers pod got deleted while the server was down or is coming up for the first time, it will not be able to start until 
the server hosting the plugin is available again.

Argo Rollouts will download the plugin at startup only once but if the pod is deleted it will need to download the plugin again on next startup. Running
Argo Rollouts in HA mode can help a little with this situation because each pod will download the plugin at startup. So if a single pod gets
deleted during a server outage, the other pods will still be able to take over because there will already be a plugin executable available to it. However,
it is up to you to define your risk for and decide how you want to install the plugin executable.

## List of Available Plugins (alphabetical order)

#### Add Your Plugin Here
  * If you have created a plugin, please submit a PR to add it to this list.
#### [sample-rollouts-metric-plugin](https://github.com/argoproj-labs/sample-rollouts-metric-plugin)
  * This is just a sample plugin that can be used as a starting point for creating your own plugin. 
It is not meant to be used in production. It is based on the built-in prometheus provider.
