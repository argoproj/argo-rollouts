# Rollouts Create Analysisrun

Create an AnalysisRun from an AnalysisTemplate or a ClusterAnalysisTemplate

## Synopsis

This command creates a new AnalysisRun from an existing AnalysisTemplate resources or from an AnalysisTemplate file.

```shell
kubectl argo rollouts create analysisrun [flags]
```

## Examples

```shell
# Create an AnalysisRun from a local AnalysisTemplate file
kubectl argo rollouts create analysisrun --from-file my-analysis-template.yaml

# Create an AnalysisRun from a AnalysisTemplate in the cluster
kubectl argo rollouts create analysisrun --from my-analysis-template

# Create an AnalysisRun from a local ClusterAnalysisTemplate file
kubectl argo rollouts create analysisrun --global --from my-analysis-cluster-template.yaml

# Create an AnalysisRun from a ClusterAnalysisTemplate in the cluster
kubectl argo rollouts create analysisrun --global --from my-analysis-cluster-template
```

## Options

```
  -a, --argument stringArray   Arguments to the parameter template
      --from string            Create an AnalysisRun from an AnalysisTemplate or ClusterAnalysisTemplate in the cluster
      --from-file string       Create an AnalysisRun from an AnalysisTemplate or ClusterAnalysisTemplate in a local file
      --generate-name string   Use the specified generateName for the run
      --global                 Use a ClusterAnalysisTemplate instead of a AnalysisTemplate
  -h, --help                   help for analysisrun
      --instance-id string     Instance-ID for the AnalysisRun
      --name string            Use the specified name for the run
```

## Options inherited from parent commands

```
      --as string                      Username to impersonate for the operation. User could be a regular user or a service account in a namespace.
      --as-group stringArray           Group to impersonate for the operation, this flag can be repeated to specify multiple groups.
      --as-uid string                  UID to impersonate for the operation.
      --cache-dir string               Default cache directory (default "$HOME/.kube/cache")
      --certificate-authority string   Path to a cert file for the certificate authority
      --client-certificate string      Path to a client certificate file for TLS
      --client-key string              Path to a client key file for TLS
      --cluster string                 The name of the kubeconfig cluster to use
      --context string                 The name of the kubeconfig context to use
      --insecure-skip-tls-verify       If true, the server's certificate will not be checked for validity. This will make your HTTPS connections insecure
  -v, --kloglevel int                  Log level for kubernetes client library
      --kubeconfig string              Path to the kubeconfig file to use for CLI requests.
      --loglevel string                Log level for kubectl argo rollouts (default "info")
  -n, --namespace string               If present, the namespace scope for this CLI request
      --request-timeout string         The length of time to wait before giving up on a single server request. Non-zero values should contain a corresponding time unit (e.g. 1s, 2m, 3h). A value of zero means don't timeout requests. (default "0")
  -s, --server string                  The address and port of the Kubernetes API server
      --tls-server-name string         Server name to use for server certificate validation. If it is not provided, the hostname used to contact the server is used
      --token string                   Bearer token for authentication to the API server
      --user string                    The name of the kubeconfig user to use
```

## See Also

* [rollouts create](kubectl-argo-rollouts_create.md)	 - Create a Rollout, Experiment, AnalysisTemplate, ClusterAnalysisTemplate, or AnalysisRun resource
