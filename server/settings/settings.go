package settings

import (
	"context"

	"sigs.k8s.io/yaml"

	settingspkg "github.com/argoproj/argo-rollouts/pkg/apiclient/settings"
	"github.com/argoproj/argo-rollouts/utils/settings"
)

// Server provides a Settings service
type Server struct {
	mgr                       *settings.SettingsManager
	authenticator             Authenticator
	disableAuth               bool
}

type Authenticator interface {
	Authenticate(ctx context.Context) (context.Context, error)
}

// NewServer returns a new instance of the Settings service
func NewServer(mgr *settings.SettingsManager, authenticator Authenticator, disableAuth bool) *Server {
	return &Server{mgr: mgr, authenticator: authenticator, disableAuth: disableAuth}
}

// Get returns Argo Rollouts settings
func (s *Server) Get(ctx context.Context, q *settingspkg.SettingsQuery) (*settingspkg.Settings, error) {
	argoRolloutSettings, err := s.mgr.GetSettings()
	if err != nil {
		return nil, err
	}
	set := settingspkg.Settings{
		URL:                argoRolloutSettings.URL,
	}

	if argoRolloutSettings.DexConfig != "" {
		var cfg settingspkg.DexConfig
		err = yaml.Unmarshal([]byte(argoRolloutSettings.DexConfig), &cfg)
		if err == nil {
			set.DexConfig = &cfg
		}
	}
	if oidcConfig := argoRolloutSettings.OIDCConfig(); oidcConfig != nil {
		set.OIDCConfig = &settingspkg.OIDCConfig{
			Name:                     oidcConfig.Name,
			Issuer:                   oidcConfig.Issuer,
			ClientID:                 oidcConfig.ClientID,
			CLIClientID:              oidcConfig.CLIClientID,
			Scopes:                   oidcConfig.RequestedScopes,
			EnablePKCEAuthentication: oidcConfig.EnablePKCEAuthentication,
		}
		if len(argoRolloutSettings.OIDCConfig().RequestedIDTokenClaims) > 0 {
			set.OIDCConfig.IDTokenClaims = argoRolloutSettings.OIDCConfig().RequestedIDTokenClaims
		}
	}
	return &set, nil
}

// AuthFuncOverride disables authentication for settings service
func (s *Server) AuthFuncOverride(ctx context.Context, fullMethodName string) (context.Context, error) {
	ctx, err := s.authenticator.Authenticate(ctx)
	if fullMethodName == "/cluster.SettingsService/Get" {
		// SettingsService/Get API is used by login page.
		// This authenticates the user, but ignores any error, so that we have claims populated
		err = nil
	}
	return ctx, err
}
