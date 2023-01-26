# Rollouts Notifications Template Notify

Generates notification using the specified template and send it to specified recipients

## Synopsis

Generates notification using the specified template and send it to specified recipients

```shell
kubectl argo rollouts notifications template notify NAME RESOURCE_NAME [flags]
```

## Examples

```shell

# Trigger notification using in-cluster config map and secret
kubectl argo rollouts notifications template notify app-sync-succeeded guestbook --recipient slack:my-slack-channel

# Render notification render generated notification in console
kubectl argo rollouts notifications template notify app-sync-succeeded guestbook

```

## Options

```
  -h, --help                    help for notify
      --recipient stringArray   List of recipients (default [console:stdout])
```

## Options inherited from parent commands

```
      --as string                      Username to impersonate for the operation
      --as-group stringArray           Group to impersonate for the operation, this flag can be repeated to specify multiple groups.
      --as-uid string                  UID to impersonate for the operation
      --cache-dir string               Default cache directory (default "$HOME/.kube/cache")
      --certificate-authority string   Path to a cert file for the certificate authority
      --client-certificate string      Path to a client certificate file for TLS
      --client-key string              Path to a client key file for TLS
      --cluster string                 The name of the kubeconfig cluster to use
      --config-map string              argo-rollouts-notification-configmap.yaml file path
      --context string                 The name of the kubeconfig context to use
      --insecure-skip-tls-verify       If true, the server's certificate will not be checked for validity. This will make your HTTPS connections insecure
  -v, --kloglevel int                  Log level for kubernetes client library
      --kubeconfig string              Path to a kube config. Only required if out-of-cluster
      --loglevel string                Log level for kubectl argo rollouts (default "info")
  -n, --namespace string               If present, the namespace scope for this CLI request
      --password string                Password for basic authentication to the API server
      --proxy-url string               If provided, this URL will be used to connect via proxy
      --request-timeout string         The length of time to wait before giving up on a single server request. Non-zero values should contain a corresponding time unit (e.g. 1s, 2m, 3h). A value of zero means don't timeout requests. (default "0")
      --secret string                  argo-rollouts-notification-secret.yaml file path. Use empty secret if provided value is ':empty'
      --server string                  The address and port of the Kubernetes API server
      --tls-server-name string         If provided, this name will be used to validate server certificate. If this is not provided, hostname used to contact the server is used.
      --token string                   Bearer token for authentication to the API server
      --user string                    The name of the kubeconfig user to use
      --username string                Username for basic authentication to the API server
```

## See Also

* [rollouts notifications template](kubectl-argo-rollouts_notifications_template.md)	 - Notification templates related commands
