# Rollouts Notifications

Set of CLI commands that helps manage notifications settings

## Synopsis

Set of CLI commands that helps manage notifications settings

```shell
kubectl argo rollouts notifications [flags]
```

## Options

```
      --config-map string   argo-rollouts-notification-configmap.yaml file path
  -h, --help                help for notifications
      --password string     Password for basic authentication to the API server
      --proxy-url string    If provided, this URL will be used to connect via proxy
      --secret string       argo-rollouts-notification-secret.yaml file path. Use empty secret if provided value is ':empty'
      --username string     Username for basic authentication to the API server
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

## Available Commands

* [rollouts notifications template](kubectl-argo-rollouts_notifications_template.md)	 - Notification templates related commands
* [rollouts notifications trigger](kubectl-argo-rollouts_notifications_trigger.md)	 - Notification triggers related commands

## See Also

* [rollouts](kubectl-argo-rollouts.md)	 - Manage argo rollouts
