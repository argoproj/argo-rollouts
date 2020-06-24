# Environment Set Up

This guide shows how to set up a local environment for development, testing, learning, or demoing
purposes.

## Helm

Some dependencies are installable via the Helm stable repository:

```shell
helm repo add stable https://kubernetes-charts.storage.googleapis.com/
helm repo update
```

## Minikube

### NGINX Ingress Controller Install

```shell
# Install nginx-ingress controller
minikube addons enable ingress

# Install prometheus, grafana, and nginx dashboards
kubectl create ns monitoring
helm install prometheus stable/prometheus -n monitoring -f docs/getting-started/setup/values-prometheus.yaml
helm install grafana stable/grafana -n monitoring -f docs/getting-started/setup/values-grafana-nginx.yaml

# Patch the ingress-nginx-controller pod so that it has the required
# prometheus annotations. This allows the pod to be scraped by the
# prometheus server.
kubectl patch deploy ingress-nginx-controller -n kube-system -p "$(cat docs/getting-started/setup/ingress-nginx-controller-metrics-scrape.yaml)"

# Grafana UI can be seen by running:
minikube service grafana -n monitoring
```

## Local Host Entries

Most ingress controllers and service mesh implementations rely on the 
[Host HTTP request header](https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Host) being
supplied in the request, in order to determine how to route the request to the correct service.
There are different ways to ensure the host is supplied with the request. In real, production
environments, this is typically achieved by adding entry for the hostname a DNS server.

On local workstations, we can sidestep the DNS changes by adding an entry to `/etc/hosts`,
to map the hostname and IP address of the ingress. First obtain the values from the ingress:

```shell
$ kubectl get ing rollouts-demo-stable
NAME                   CLASS    HOSTS                 ADDRESS        PORTS   AGE
rollouts-demo-stable   <none>   rollouts-demo.local   192.168.64.2   80      80m
```

From the above output, the correct entry in `/etc/hosts` would be:

```
rollouts-demo.local   192.168.64.2
```