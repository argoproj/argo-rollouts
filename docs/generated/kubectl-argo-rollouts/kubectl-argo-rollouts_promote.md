# Rollouts Promote

Promote a rollout

## Synopsis

Promote a rollout

Promotes a rollout paused at a canary step, or a paused blue-green pre-promotion.
To skip analysis, pauses and steps entirely, use '--full' to fully promote the rollout

```shell
kubectl argo rollouts promote ROLLOUT_NAME [flags]
```

## Examples

```shell
# Promote a paused rollout
kubectl argo rollouts promote guestbook

# Fully promote a rollout to desired version, skipping analysis, pauses, and steps
kubectl argo rollouts promote guestbook --full
```

## Options

```
      --full   Perform a full promotion, skipping analysis, pauses, and steps
  -h, --help   help for promote
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

* [rollouts](kubectl-argo-rollouts.md)	 - Manage argo rollouts
