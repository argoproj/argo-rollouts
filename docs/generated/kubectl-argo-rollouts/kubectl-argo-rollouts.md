# Rollouts

Manage argo rollouts

## Synopsis

This command consists of multiple subcommands which can be used to manage Argo Rollouts.

```shell
kubectl argo rollouts COMMAND [flags]
```

## Examples

```shell
# Get guestbook rollout and watch progress
kubectl argo rollouts get rollout guestbook -w

# Pause the guestbook rollout
kubectl argo rollouts pause guestbook

# Promote the guestbook rollout
kubectl argo rollouts promote guestbook

# Abort the guestbook rollout
kubectl argo rollouts abort guestbook

# Retry the guestbook rollout
kubectl argo rollouts retry guestbook
```

## Options

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
  -h, --help                           help for kubectl-argo-rollouts
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

* [rollouts abort](kubectl-argo-rollouts_abort.md)	 - Abort a rollout
* [rollouts completion](kubectl-argo-rollouts_completion.md)	 - Generate completion script
* [rollouts create](kubectl-argo-rollouts_create.md)	 - Create a Rollout, Experiment, AnalysisTemplate, ClusterAnalysisTemplate, or AnalysisRun resource
* [rollouts dashboard](kubectl-argo-rollouts_dashboard.md)	 - Start UI dashboard
* [rollouts get](kubectl-argo-rollouts_get.md)	 - Get details about rollouts and experiments
* [rollouts lint](kubectl-argo-rollouts_lint.md)	 - Lint and validate a Rollout
* [rollouts list](kubectl-argo-rollouts_list.md)	 - List rollouts or experiments
* [rollouts notifications](kubectl-argo-rollouts_notifications.md)	 - Set of CLI commands that helps manage notifications settings
* [rollouts pause](kubectl-argo-rollouts_pause.md)	 - Pause a rollout
* [rollouts promote](kubectl-argo-rollouts_promote.md)	 - Promote a rollout
* [rollouts restart](kubectl-argo-rollouts_restart.md)	 - Restart the pods of a rollout
* [rollouts retry](kubectl-argo-rollouts_retry.md)	 - Retry a rollout or experiment
* [rollouts set](kubectl-argo-rollouts_set.md)	 - Update various values on resources
* [rollouts status](kubectl-argo-rollouts_status.md)	 - Show the status of a rollout
* [rollouts terminate](kubectl-argo-rollouts_terminate.md)	 - Terminate an AnalysisRun or Experiment
* [rollouts undo](kubectl-argo-rollouts_undo.md)	 - Undo a rollout
* [rollouts version](kubectl-argo-rollouts_version.md)	 - Print version

