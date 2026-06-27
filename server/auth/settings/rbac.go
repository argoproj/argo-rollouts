package settings

import "context"

// RBAC ConfigMap and auth-flag data keys.
const (
	KeyPolicyCSV        = "policy.csv"
	KeyPolicyDefault    = "policy.default"
	KeyPolicyMatchMode  = "policy.matchMode"
	KeyAnonymousEnabled = "users.anonymous.enabled"
	KeyURL              = "url"
)

// defaultMatchMode is used when policy.matchMode is unset.
const defaultMatchMode = "glob"

// RBACConfig holds the dashboard's RBAC policy configuration, suitable for
// feeding into the rbac package's enforcer.
type RBACConfig struct {
	PolicyCSV   string
	DefaultRole string
	MatchMode   string
}

// GetRBACConfig reads the RBAC ConfigMap. MatchMode defaults to "glob".
func (m *SettingsManager) GetRBACConfig(ctx context.Context) (RBACConfig, error) {
	data, err := m.configMapData(ctx, RBACConfigMapName)
	if err != nil {
		return RBACConfig{}, err
	}
	matchMode := data[KeyPolicyMatchMode]
	if matchMode == "" {
		matchMode = defaultMatchMode
	}
	return RBACConfig{
		PolicyCSV:   data[KeyPolicyCSV],
		DefaultRole: data[KeyPolicyDefault],
		MatchMode:   matchMode,
	}, nil
}

// AnonymousEnabled reports whether unauthenticated access is enabled (default false).
func (m *SettingsManager) AnonymousEnabled(ctx context.Context) (bool, error) {
	data, err := m.configMapData(ctx, ConfigMapName)
	if err != nil {
		return false, err
	}
	return parseBoolDefault(data[KeyAnonymousEnabled], false), nil
}

// GetURL returns the configured external dashboard URL (empty if unset).
func (m *SettingsManager) GetURL(ctx context.Context) (string, error) {
	data, err := m.configMapData(ctx, ConfigMapName)
	if err != nil {
		return "", err
	}
	return data[KeyURL], nil
}
