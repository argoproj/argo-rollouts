# Environment Set Up

This guide shows how to set up a local environment for development, testing, learning, or demoing
purposes.

## Helm

Some dependencies are installable via the Helm stable repository:

```shell
helm repo add stable https://charts.helm.sh/stable
helm repo add grafana https://grafana.github.io/helm-charts
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update
```

## Minikube

### NGINX Ingress Controller Setup

The following instructions describe how to configure NGINX Ingress Controller on minikube. For
basic ingress support, only the "ingress" addon needs to be enabled:

```shell
minikube addons enable ingress
```

Optionally, Prometheus and Grafana can be installed to utilize progressive delivery functionality:

```shell
# Install Prometheus
kubectl create ns monitoring
helm install prometheus prometheus-community/prometheus -n monitoring -f docs/getting-started/setup/values-prometheus.yaml

# Patch the ingress-nginx-controller pod so that it has the required
# prometheus annotations. This allows the pod to be scraped by the
# prometheus server.
kubectl patch deploy ingress-nginx-controller -n ingress-nginx -p "$(cat docs/getting-started/setup/ingress-nginx-controller-metrics-scrape.yaml)"
```

!!! note
    [For Minikube version 1.18.1 or earlier](https://kubernetes.io/docs/tasks/access-application-cluster/ingress-minikube/#enable-the-ingress-controller), 
    change the `-n` parameter value (namespace) to `kube-system`.

```shell
# Install grafana along with nginx ingress dashboards
helm install grafana grafana/grafana -n monitoring -f docs/getting-started/setup/values-grafana-nginx.yaml

# Grafana UI can be accessed by running:
minikube service grafana -n monitoring
```

### Istio Setup

The following instructions describe how to configure Istio on minikube.

```shell
# Istio on Minikube requires additional memory and CPU
minikube start --memory=8192mb --cpus=4

# Install istio
minikube addons enable istio-provisioner
minikube addons enable istio

# Label the default namespace to enable istio sidecar injection for the namespace
kubectl label namespace default istio-injection=enabled
```

Istio already comes with a Prometheus database ready to use. To visualize metrics about istio
services, Grafana and Istio dashboards can be installed via Helm to leverage progressive delivery
functionality:

```
# Install Grafana and Istio dashboards
helm install grafana grafana/grafana -n istio-system -f docs/getting-started/setup/values-grafana-istio.yaml

# Grafana UI can be accessed by running
minikube service grafana -n istio-system
```

In order for traffic to enter the Istio mesh, the request needs to go through an Istio ingress
gateway, which is simply a normal Kubernetes Deployment and Service. One convenient way to reach
the gateway using minikube, is using the `minikube tunnel` command, which assigns Services a 
LoadBalancer. This command should be run in the background, usually in a separate terminal window:

```shell
minikube tunnel
```

While running `minikube tunnel`, the `istio-ingressgateway` Service will now have an external IP
which can be retrieved via `kubectl`:

```shell
$ kubectl get svc -n istio-system istio-ingressgateway
NAME                   TYPE           CLUSTER-IP      EXTERNAL-IP     PORT(S)                            AGE
istio-ingressgateway   LoadBalancer   10.100.136.45   10.100.136.45   15020:31711/TCP,80:31298/TCP....   7d22h
```

The LoadBalancer external IP (10.100.136.45 in this example) is now reachable to access services in
the Istio mesh. Istio routes requests to the correct pod based on the Host HTTP header. Follow the
guide on [supplying host headers](#supplying-host-headers) to learn how to configure your client
environment to supply the proper request to reach the pod.

### Linkerd Setup

Linkerd can be installed using the linkerd CLI.

```
brew install linkerd
linkerd install | kubectl apply -f -
```

Linkerd does not provide its own ingress controller, choosing instead to work alongside your
ingress controller of choice. On minikube, we can use the built-in NGINX ingress addon and
reconfigure it to be part of the linkerd mesh.

```
# Install the NGINX ingress controller addon
minikube addons enable ingress

# Patch the nginx-ingress-controller deployment to allow injection of the linkerd proxy to the
# pod, so that it will be part of the mesh.
kubectl patch deploy ingress-nginx-controller -n kube-system \
  -p '{"spec":{"template":{"metadata":{"annotations":{"linkerd.io/inject":"enabled"}}}}}'
```

## Supplying Host Headers

Most ingress controllers and service mesh implementations rely on the 
[Host HTTP request header](https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Host) being
supplied in the request in order to determine how to route the request to the correct pod.

### Determining the hostname to IP mapping

For the Host header to be set in the request, the hostname of the service should resolve to the
public IP address of the ingress or service mesh. Depending on if you are using an ingress
controller or a service mesh, use one of the following techniques to determine the correct hostname
to IP mapping:

#### Ingresses

For traffic which is reaching the cluster network via a normal Kubernetes Ingress, the hostname
should map to the IP of the ingress. We can retrieve the external IP of the ingress from the
Ingress object itself, using `kubectl`:

```shell
$ kubectl get ing rollouts-demo-stable
NAME                   CLASS    HOSTS                 ADDRESS        PORTS   AGE
rollouts-demo-stable   <none>   rollouts-demo.local   192.168.64.2   80      80m
```

In the example above, the hostname `rollouts-demo.local` should be configured to resolve to the
IP `192.168.64.2`. The next section describes various ways to configure your local system to
resolve the hostname to the desired IP.

#### Istio

In the case of Istio, traffic enters the mesh through an
[Ingress Gateway](https://istio.io/latest/docs/tasks/traffic-management/ingress/ingress-control/),
which simply is a load balancer sitting at the edge of mesh.

To determine the correct hostname to IP mapping, it largely depends on what was configured in the 
`VirtualService` and `Gateway`. If you are following the
[Istio getting started guide](../istio/index.md), the examples use the "default" istio 
ingress gateway, which we can obtain from kubectl:

```shell
$ kubectl get svc -n istio-system istio-ingressgateway
NAME                   TYPE           CLUSTER-IP      EXTERNAL-IP     PORT(S)                            AGE
istio-ingressgateway   LoadBalancer   10.100.136.45   10.100.136.45   15020:31711/TCP,80:31298/TCP....   7d22h
```

In the above example, the hostname `rollouts-demo.local` should be configured to resolve to the
IP `10.100.136.45`. The next section describes various ways to configure your local system to
resolve the hostname to the desired IP.

### Configuring local hostname resolution

Now that you have determined the correct hostname to IP mapping, the next step involves configuring
the system so that will resolve properly. There are different techniques to do this:

#### DNS Entry

In real, production environments, the Host header is typically achieved by adding a DNS entry for
the hostname in the DNS server. However, for local development, this is typically not an easily
accessible option.

#### /etc/hosts Entry

On local workstations, a local entry to `/etc/hosts` can be added to map the hostname and IP address
of the ingress. For example, the following is an example of an `/etc/hosts` file which maps
`rollouts-demo.local` to IP `10.100.136.45`.

```shell
##
# Host Database
#
# localhost is used to configure the loopback interface
# when the system is booting.  Do not change this entry.
##
127.0.0.1       localhost
255.255.255.255 broadcasthost
::1             localhost

10.100.136.45  rollouts-demo.local
```

The advantages of using a host entry, are that it works for all clients (CLIs, browsers). On the
other hand, it is harder to maintain if the IP address changes frequently.

#### Supply Header in Curl

Clients such as curl, have the ability to explicitly set a header (the `-H` flag in curl).
For example:

```shell
$ curl -I -H 'Host: rollouts-demo.local' http://10.100.136.45/color
HTTP/1.1 200 OK
content-type: text/plain; charset=utf-8
x-content-type-options: nosniff
date: Wed, 24 Jun 2020 08:44:59 GMT
content-length: 6
x-envoy-upstream-service-time: 1
server: istio-envoy
```

Notice that the same request made *without* the header, fails with a `404 Not Found` error.

```shell
$ curl -I http://10.100.136.45/color
HTTP/1.1 404 Not Found
date: Wed, 24 Jun 2020 08:46:07 GMT
server: istio-envoy
transfer-encoding: chunked
```

#### Browser Extension

Similar to curl's ability to explicitly set a header, browsers can also achieve this via browser
extensions. One example of a browser extension which can do this, is
[ModHeader](https://bewisse.com/modheader/).
