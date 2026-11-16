# Argo-Rollouts

argo-rollouts is a controller to operate on rollout CRD

## Synopsis

argo-rollouts is a controller to operate on rollout CRD

```shell
argo-rollouts [flags]
```

## Options

```
      --alb-ingress-classes stringArray               Defines all the ingress class annotations that the alb ingress controller operates on. Defaults to alb (default [alb])
      --alb-tag-key-resource-id string                Set the default AWS LoadBalancer tag key for resource ID that controller uses when verifying target group weights. (default "ingress.k8s.aws/resource")
      --ambassador-api-version string                 Set the Ambassador apiVersion that controller should look when manipulating Ambassador Mappings. (default "getambassador.io/v2")
      --analysis-shared-secret-mode string            Controls which secrets in the shared-secret namespace AnalysisRuns may read: 'LabelFilter' (default, only labeled secrets) or 'AllSecrets' (any secret in the namespace) (default "LabelFilter")
      --analysis-shared-secret-namespace string       Namespace containing secrets that may be referenced by AnalysisRuns
      --analysis-threads int                          Set the number of worker threads for the Analysis controller (default 30)
      --appmesh-crd-version string                    Set the default AppMesh CRD Version that controller uses when manipulating resources. (default "v1beta2")
      --as string                                     Username to impersonate for the operation
      --as-group stringArray                          Group to impersonate for the operation, this flag can be repeated to specify multiple groups.
      --as-uid string                                 UID to impersonate for the operation
      --aws-target-group-binding-api-version string   Set the default AWS TargetGroupBinding apiVersion that controller uses when verifying target group weights. (default "elbv2.k8s.aws/v1beta1")
      --aws-verify-target-group                       Verify ALB target group before progressing through steps (requires AWS privileges)
      --burst int                                     Maximum burst for throttle. (default 80)
      --certificate-authority string                  Path to a cert file for the certificate authority
      --client-certificate string                     Path to a client certificate file for TLS
      --client-key string                             Path to a client key file for TLS
      --cluster string                                The name of the kubeconfig cluster to use
      --context string                                The name of the kubeconfig context to use
      --controllers strings                           Explicitly specify the list of controllers to run, currently only supports 'analysis', eg. --controllers=analysis. Default: all controllers are enabled
      --disable-compression                           If true, opt-out of response compression for all requests to the server
      --enable-pprof-address string                   Enable pprof profiling on controller by providing a server address.
      --ephemeral-metadata-threads int                Set the number of worker threads for the Ephemeral Metadata reconciler (default 10)
      --ephemeral-metadata-update-pod-retries int     Set the number of retries to update pod Ephemeral Metadata (default 3)
      --experiment-threads int                        Set the number of worker threads for the Experiment controller (default 10)
      --healthzPort int                               Set the port the healthz endpoint should be exposed over (default 8080)
  -h, --help                                          help for argo-rollouts
      --ingress-api-version string                    Set the Ingress apiVersion that the controller should use.
      --ingress-threads int                           Set the number of worker threads for the Ingress controller (default 10)
      --insecure-skip-tls-verify                      If true, the server's certificate will not be checked for validity. This will make your HTTPS connections insecure
      --instance-id string                            Indicates which argo rollout objects the controller should operate on
      --istio-api-version string                      Set the default Istio apiVersion that controller should look when manipulating VirtualServices. (default "v1alpha3")
      --kloglevel int                                 Set the klog logging level
      --kubeconfig string                             Path to a kube config. Only required if out-of-cluster
      --leader-elect                                  If true, controller will perform leader election between instances to ensure no more than one instance of controller operates at a time (default true)
      --leader-election-lease-duration duration       The duration that non-leader candidates will wait after observing a leadership renewal until attempting to acquire leadership of a led but unrenewed leader slot. This is effectively the maximum duration that a leader can be stopped before it is replaced by another candidate. This is only applicable if leader election is enabled. (default 15s)
      --leader-election-renew-deadline duration       The interval between attempts by the acting master to renew a leadership slot before it stops leading. This must be less than or equal to the lease duration. This is only applicable if leader election is enabled. (default 10s)
      --leader-election-retry-period duration         The duration the clients should wait between attempting acquisition and renewal of a leadership. This is only applicable if leader election is enabled. (default 2s)
      --logformat string                              Set the logging format. One of: text|json
      --loglevel string                               Set the logging level. One of: debug|info|warn|error (default "info")
      --metricsPort int                               Set the port the metrics endpoint should be exposed over (default 8090)
  -n, --namespace string                              If present, the namespace scope for this CLI request
      --namespaced                                    runs controller in namespaced mode (does not require cluster RBAC)
      --nginx-ingress-classes stringArray             Defines all the ingress class annotations that the nginx ingress controller operates on. Defaults to nginx (default [nginx])
      --password string                               Password for basic authentication to the API server
      --proxy-url string                              If provided, this URL will be used to connect via proxy
      --qps float32                                   Maximum QPS (queries per second) to the K8s API server (default 40)
      --request-timeout string                        The length of time to wait before giving up on a single server request. Non-zero values should contain a corresponding time unit (e.g. 1s, 2m, 3h). A value of zero means don't timeout requests. (default "0")
      --rollout-resync int                            Time period in seconds for rollouts resync. (default 900)
      --rollout-threads int                           Set the number of worker threads for the Rollout controller (default 10)
      --self-service-notification-enabled             Allows rollouts controller to pull notification config from the namespace that the rollout resource is in. This is useful for self-service notification.
      --server string                                 The address and port of the Kubernetes API server
      --service-threads int                           Set the number of worker threads for the Service controller (default 10)
      --tls-server-name string                        If provided, this name will be used to validate server certificate. If this is not provided, hostname used to contact the server is used.
      --token string                                  Bearer token for authentication to the API server
      --traefik-api-group string                      Set the default Traefik apiGroup that controller uses. (default "traefik.io")
      --traefik-api-version string                    Set the default Traefik apiVersion that controller uses. (default "traefik.io/v1alpha1")
      --traffic-split-api-version string              Set the default TrafficSplit apiVersion that controller uses when creating TrafficSplits. (default "v1alpha1")
      --user string                                   The name of the kubeconfig user to use
      --username string                               Username for basic authentication to the API server
      --version                                       Print version
```

