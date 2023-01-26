# Rollouts Completion

Generate completion script

## Synopsis

To load completions:
	
	Bash:
	
	  $ source <(yourprogram completion bash)
	
	  # To load completions for each session, execute once:
	  # Linux:
	  $ yourprogram completion bash > /etc/bash_completion.d/yourprogram
	  # macOS:
	  $ yourprogram completion bash > /usr/local/etc/bash_completion.d/yourprogram
	
	Zsh:
	
	  # If shell completion is not already enabled in your environment,
	  # you will need to enable it.  You can execute the following once:
	
	  $ echo "autoload -U compinit; compinit" >> ~/.zshrc
	
	  # To load completions for each session, execute once:
	  $ yourprogram completion zsh > "${fpath[1]}/_yourprogram"
	
	  # You will need to start a new shell for this setup to take effect.
	
	fish:
	
	  $ yourprogram completion fish | source
	
	  # To load completions for each session, execute once:
	  $ yourprogram completion fish > ~/.config/fish/completions/yourprogram.fish
	
	PowerShell:
	
	  PS> yourprogram completion powershell | Out-String | Invoke-Expression
	
	  # To load completions for every new session, run:
	  PS> yourprogram completion powershell > yourprogram.ps1
	  # and source this file from your PowerShell profile.
	

```shell
kubectl argo rollouts completion [bash|zsh|fish|powershell]
```

## Options

```
  -h, --help   help for completion
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
