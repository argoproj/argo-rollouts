package settings

import (
	"context"
	"fmt"

	"sigs.k8s.io/yaml"
)

// KeyOIDCConfig is the argo-rollouts-dashboard-cm key holding the OIDC config YAML.
const KeyOIDCConfig = "oidc.config"

// OIDCConfig is the dashboard's OIDC provider configuration.
type OIDCConfig struct {
	Issuer          string   `json:"issuer"`
	ClientID        string   `json:"clientID"`
	ClientSecret    string   `json:"clientSecret"`
	RequestedScopes []string `json:"requestedScopes,omitempty"`
}

var defaultOIDCScopes = []string{"openid", "profile", "email", "groups"}

// GetOIDCConfig parses the oidc.config entry. Returns (nil, false, nil) when not
// configured, and an error when present but malformed.
func (m *SettingsManager) GetOIDCConfig(ctx context.Context) (*OIDCConfig, bool, error) {
	data, err := m.configMapData(ctx, ConfigMapName)
	if err != nil {
		return nil, false, err
	}
	raw := data[KeyOIDCConfig]
	if raw == "" {
		return nil, false, nil
	}
	var cfg OIDCConfig
	if err := yaml.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil, false, fmt.Errorf("parse %s: %w", KeyOIDCConfig, err)
	}
	if len(cfg.RequestedScopes) == 0 {
		cfg.RequestedScopes = defaultOIDCScopes
	}
	return &cfg, true, nil
}
