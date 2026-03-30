# Rollouts Dashboard

Start UI dashboard

## Synopsis

Start UI dashboard

```shell
kubectl argo rollouts dashboard [flags]
```

## Examples

```shell
# Start UI dashboard
kubectl argo rollouts dashboard

# Start UI dashboard on a specific port
kubectl argo rollouts dashboard --port 8080

# Start UI dashboard with token authentication
kubectl argo rollouts dashboard --auth-mode token

# Start UI dashboard with OIDC SSO (e.g., Okta, Google, Azure AD)
kubectl argo rollouts dashboard --auth-mode token \
--oidc-issuer-url https://accounts.google.com \
--oidc-client-id my-client-id \
--oidc-client-secret my-client-secret \
--oidc-redirect-url http://localhost:3100/rollouts/auth/callback
```

## Options

```
      --auth-mode string            authentication mode: 'server' (no auth) or 'token' (requires Kubernetes bearer token) (default "server")
  -h, --help                        help for dashboard
      --oidc-client-id string       OIDC client ID
      --oidc-client-secret string   OIDC client secret
      --oidc-issuer-url string      OIDC issuer URL for SSO login (e.g., https://accounts.google.com, https://your-org.okta.com)
      --oidc-redirect-url string    OIDC redirect URL (default: http://localhost:<port>/<root-path>/auth/callback)
      --oidc-scopes string          OIDC scopes as comma-separated list (default: openid,profile,email)
  -p, --port int                    port to listen on (default 3100)
      --root-path string            changes the root path of the dashboard (default "rollouts")
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
      --disable-compression            If true, opt-out of response compression for all requests to the server
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
