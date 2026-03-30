package dashboard

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options"
	"github.com/argoproj/argo-rollouts/server"
)

var (
	dashBoardExample = `
	# Start UI dashboard
	%[1]s dashboard

	# Start UI dashboard on a specific port
	%[1]s dashboard --port 8080

	# Start UI dashboard with token authentication
	%[1]s dashboard --auth-mode token

	# Start UI dashboard with OIDC SSO (e.g., Okta, Google, Azure AD)
	%[1]s dashboard --auth-mode token \
		--oidc-issuer-url https://accounts.google.com \
		--oidc-client-id my-client-id \
		--oidc-client-secret my-client-secret \
		--oidc-redirect-url http://localhost:3100/rollouts/auth/callback`
)

func NewCmdDashboard(o *options.ArgoRolloutsOptions) *cobra.Command {
	var rootPath string
	var port int
	var authMode string
	var oidcIssuerURL string
	var oidcClientID string
	var oidcClientSecret string
	var oidcRedirectURL string
	var oidcScopes string
	var cmd = &cobra.Command{
		Use:     "dashboard",
		Short:   "Start UI dashboard",
		Example: o.Example(dashBoardExample),
		RunE: func(c *cobra.Command, args []string) error {
			if authMode != server.AuthModeServer && authMode != server.AuthModeToken {
				return fmt.Errorf("invalid auth mode %q: must be %q or %q", authMode, server.AuthModeServer, server.AuthModeToken)
			}

			if oidcIssuerURL != "" && authMode != server.AuthModeToken {
				return fmt.Errorf("--oidc-issuer-url requires --auth-mode=token")
			}

			namespace := o.Namespace()
			kubeclientset := o.KubeClientset()
			rolloutclientset := o.RolloutsClientset()

			// Get REST config for per-request client creation (RBAC enforcement)
			restConfig, err := o.RESTClientGetter.ToRESTConfig()
			if err != nil {
				return fmt.Errorf("failed to get REST config: %w", err)
			}

			var scopes []string
			if oidcScopes != "" {
				scopes = strings.Split(oidcScopes, ",")
			}

			opts := server.ServerOptions{
				Namespace:         namespace,
				KubeClientset:     kubeclientset,
				RolloutsClientset: rolloutclientset,
				DynamicClientset:  o.DynamicClientset(),
				RootPath:          rootPath,
				AuthMode:          authMode,
				RESTConfig:        restConfig,
				OIDCConfig: server.OIDCConfig{
					IssuerURL:    oidcIssuerURL,
					ClientID:     oidcClientID,
					ClientSecret: oidcClientSecret,
					RedirectURL:  oidcRedirectURL,
					Scopes:       scopes,
				},
			}

			for {
				ctx := context.Background()
				ctx, cancel := context.WithCancel(ctx)
				argorollouts := server.NewServer(opts)
				argorollouts.Run(ctx, port, true)
				cancel()
			}
		},
	}
	cmd.Flags().StringVar(&rootPath, "root-path", "rollouts", "changes the root path of the dashboard")
	cmd.Flags().IntVarP(&port, "port", "p", 3100, "port to listen on")
	cmd.Flags().StringVar(&authMode, "auth-mode", server.AuthModeServer, "authentication mode: 'server' (no auth) or 'token' (requires Kubernetes bearer token)")
	cmd.Flags().StringVar(&oidcIssuerURL, "oidc-issuer-url", "", "OIDC issuer URL for SSO login (e.g., https://accounts.google.com, https://your-org.okta.com)")
	cmd.Flags().StringVar(&oidcClientID, "oidc-client-id", "", "OIDC client ID")
	cmd.Flags().StringVar(&oidcClientSecret, "oidc-client-secret", "", "OIDC client secret")
	cmd.Flags().StringVar(&oidcRedirectURL, "oidc-redirect-url", "", "OIDC redirect URL (default: http://localhost:<port>/<root-path>/auth/callback)")
	cmd.Flags().StringVar(&oidcScopes, "oidc-scopes", "", "OIDC scopes as comma-separated list (default: openid,profile,email)")

	return cmd
}
