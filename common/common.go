package common

import (
	"errors"
	"os"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	ArgoRolloutsConfigMapName              = "argo-rollouts-cm"
	ArgoRolloutsSecretName                 = "argo-rollouts-secret"
	ArgoRolloutsRBACConfigMapName          = "argo-rollouts-rbac-cm"

  // DefaultSSOLocalPort is the localhost port to listen on for the temporary web server performing
  // the OAuth2 login flow.
  DefaultSSOLocalPort = 8085

	// ArgoRolloutsTLSCertsConfigMapName contains TLS certificate data for connecting repositories. Will get mounted as volume to pods
	ArgoRolloutsTLSCertsConfigMapName = "argorollouts-tls-certs-cm"
)

// Default paths on the pod's file system
const (
	// DefaultPathTLSConfig is the default path where TLS certificates for repositories are located
	DefaultPathTLSConfig = "/app/config/tls"
  	// DefaultPathSSHConfig is the default path where SSH known hosts are stored
	DefaultPathSSHConfig = "/app/config/ssh"
  // DefaultSSHKnownHostsName is the Default name for the SSH known hosts file
	DefaultSSHKnownHostsName = "ssh_known_hosts"
)

// Default service addresses and URLS of Argo Rollouts internal services
const (
	// DefaultRedisAddr is the default redis address
	DefaultRedisAddr = "argorollouts-redis:6379"
)

const (
	// CacheVersion is a objects version cached using util/cache/cache.go.
	// Number should be bumped in case of backward incompatible change to make sure cache is invalidated after upgrade.
	CacheVersion = "1.8.3"
)

// Dex related constants
const (
	// DexAPIEndpoint is the endpoint where we serve the Dex API server
	DexAPIEndpoint = "/api/dex"
	// LoginEndpoint is Argo CD's shorthand login endpoint which redirects to dex's OAuth 2.0 provider's consent page
	LoginEndpoint = "/auth/login"
	// LogoutEndpoint is Argo CD's shorthand logout endpoint which invalidates OIDC session after logout
	LogoutEndpoint = "/auth/logout"
	// CallbackEndpoint is Argo CD's final callback endpoint we reach after OAuth 2.0 login flow has been completed
	CallbackEndpoint = "/auth/callback"
	// DexCallbackEndpoint is Argo CD's final callback endpoint when Dex is configured
	DexCallbackEndpoint = "/api/dex/callback"
	// ArgoRolloutsClientAppName is name of the Oauth client app used when registering our web app to dex
	ArgoRolloutsClientAppName = "Argo Rollouts"
	// ArgoRolloutsClientAppID is the Oauth client ID we will use when registering our app to dex
	ArgoRolloutsClientAppID = "argo-rollouts"
	// ArgoRolloutsCLIClientAppName is name of the Oauth client app used when registering our CLI to dex
	ArgoRolloutsCLIClientAppName = "Argo Rollout CLI"
	// ArgoRolloutsCLIClientAppID is the Oauth client ID we will use when registering our CLI to dex
	ArgoRolloutsCLIClientAppID = "argo-rollouts-cli"
)

// Environment variables for tuning and debugging Argo Rollout
const (
	// EnvVarSSODebug is an environment variable to enable additional OAuth debugging in the API server
	EnvVarSSODebug = "ARGOROLLOUTS_SSO_DEBUG"
	// EnvVarRBACDebug is an environment variable to enable additional RBAC debugging in the API server
	EnvVarRBACDebug = "ARGOROLLOUTS_RBAC_DEBUG"
	// EnvVarSSHDataPath overrides the location where SSH known hosts for repo access data is stored
	EnvVarSSHDataPath = "ARGOROLLOUTS_SSH_DATA_PATH"
	// EnvVarTLSDataPath overrides the location where TLS certificate for repo access data is stored
	EnvVarTLSDataPath = "ARGOROLLOUTS_TLS_DATA_PATH"
  // EnvMaxCookieNumber max number of chunks a cookie can be broken into
	EnvMaxCookieNumber = "ARGOROLLOUTS_MAX_COOKIE_NUMBER"
  	// EnvLogFormat log format that is defined by `--logformat` option
	EnvLogFormat = "ARGOROLLOUTS_LOG_FORMAT"
	// EnvLogLevel log level that is defined by `--loglevel` option
	EnvLogLevel = "ARGOROLLOUTS_LOG_LEVEL"
  // EnvLogFormatEnableFullTimestamp enables the FullTimestamp option in logs
	EnvLogFormatEnableFullTimestamp = "ARGOROLLOUTS_LOG_FORMAT_ENABLE_FULL_TIMESTAMP"
	// EnvGRPCKeepAliveMin defines the GRPCKeepAliveEnforcementMinimum, used in the grpc.KeepaliveEnforcementPolicy. Expects a "Duration" format (e.g. 10s).
	EnvGRPCKeepAliveMin = "ARGOROLLOUTS_GRPC_KEEP_ALIVE_MIN"
)

// Argo Rollouts related constants
const (
	// AuthCookieName is the HTTP cookie name where we store our auth token
	AuthCookieName = "argorollouts.token"
	// StateCookieName is the HTTP cookie name that holds temporary nonce tokens for CSRF protection
	StateCookieName = "argorollouts.oauthstate"
	// StateCookieMaxAge is the maximum age of the oauth state cookie
	StateCookieMaxAge = time.Minute * 5
)

// Resource metadata labels and annotations (keys and values) used by Argo CD components
const (
	// LabelKeyAppInstance is the label key to use to uniquely identify the instance of an application
	// The Argo CD application name is used as the instance name
	LabelKeyAppInstance = "app.kubernetes.io/instance"
	// LabelKeyAppName is the label key to use to uniquely identify the name of the Kubernetes application
	LabelKeyAppName = "app.kubernetes.io/name"
	// LabelKeyAutoLabelClusterInfo if set to true will automatically add extra labels from the cluster info (currently it only adds a k8s version label)
	LabelKeyAutoLabelClusterInfo = "argorollouts.argoproj.io/auto-label-cluster-info"
	// LabelKeyLegacyApplicationName is the legacy label (v0.10 and below) and is superseded by 'app.kubernetes.io/instance'
	LabelKeyLegacyApplicationName = "applications.argoproj.io/app-name"
	// LabelKeySecretType contains the type of argorollouts secret (currently: 'cluster', 'repository', 'repo-config' or 'repo-creds')
	LabelKeySecretType = "argorollouts.argoproj.io/secret-type"
	// LabelKeyClusterKubernetesVersion contains the kubernetes version of the cluster secret if it has been enabled
	LabelKeyClusterKubernetesVersion = "argorollouts.argoproj.io/kubernetes-version"
	// LabelValueSecretTypeCluster indicates a secret type of cluster
	LabelValueSecretTypeCluster = "cluster"
	// LabelValueSecretTypeRepository indicates a secret type of repository
	LabelValueSecretTypeRepository = "repository"
	// LabelValueSecretTypeRepoCreds indicates a secret type of repository credentials
	LabelValueSecretTypeRepoCreds = "repo-creds"
)

// TokenVerificationError is a generic error message for a failure to verify a JWT
const TokenVerificationError = "failed to verify the token"

var TokenVerificationErr = errors.New(TokenVerificationError)

// Security severity logging
const (
	SecurityField = "security"
	// SecurityCWEField is the logs field for the CWE associated with a log line. CWE stands for Common Weakness Enumeration. See https://cwe.mitre.org/
	SecurityCWEField                          = "CWE"
	SecurityCWEIncompleteCleanup              = 459
	SecurityCWEMissingReleaseOfFileDescriptor = 775
	SecurityEmergency                         = 5 // Indicates unmistakably malicious events that should NEVER occur accidentally and indicates an active attack (i.e. brute forcing, DoS)
	SecurityCritical                          = 4 // Indicates any malicious or exploitable event that had a side effect (i.e. secrets being left behind on the filesystem)
	SecurityHigh                              = 3 // Indicates likely malicious events but one that had no side effects or was blocked (i.e. out of bounds symlinks in repos)
	SecurityMedium                            = 2 // Could indicate malicious events, but has a high likelihood of being user/system error (i.e. access denied)
	SecurityLow                               = 1 // Unexceptional entries (i.e. successful access logs)
)

// gRPC settings
const (
	defaultGRPCKeepAliveEnforcementMinimum = 10 * time.Second
)

func GetGRPCKeepAliveEnforcementMinimum() time.Duration {
	if GRPCKeepAliveMinStr := os.Getenv(EnvGRPCKeepAliveMin); GRPCKeepAliveMinStr != "" {
		GRPCKeepAliveMin, err := time.ParseDuration(GRPCKeepAliveMinStr)
		if err != nil {
			logrus.Warnf("invalid env var value for %s: cannot parse: %s. Default value %s will be used.", EnvGRPCKeepAliveMin, err, defaultGRPCKeepAliveEnforcementMinimum)
			return defaultGRPCKeepAliveEnforcementMinimum
		}
		return GRPCKeepAliveMin
	}
	return defaultGRPCKeepAliveEnforcementMinimum
}
