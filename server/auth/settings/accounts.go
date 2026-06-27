package settings

import (
	"context"
	"fmt"
	"strconv"

	"github.com/argoproj/argo-rollouts/server/auth/password"
)

// Account-related data keys.
const (
	KeyAdminPassword = "admin.password"
	KeyAdminEnabled  = "admin.enabled"
)

// AdminUsername is the built-in administrator account name.
const AdminUsername = "admin"

// Account is a local dashboard account.
type Account struct {
	Enabled      bool
	PasswordHash string
}

// parseBoolDefault returns the parsed boolean for raw, or def if raw is empty
// or unparseable.
func parseBoolDefault(raw string, def bool) bool {
	if raw == "" {
		return def
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return def
	}
	return v
}

// GetAccount returns the named local account. The admin account is read from
// admin.password / admin.enabled; other accounts from accounts.<name>.password
// / accounts.<name>.enabled. Accounts are enabled by default. An account with
// no stored password hash is reported as not found.
func (m *SettingsManager) GetAccount(ctx context.Context, name string) (Account, error) {
	secret, err := m.secretData(ctx)
	if err != nil {
		return Account{}, err
	}
	cm, err := m.configMapData(ctx, ConfigMapName)
	if err != nil {
		return Account{}, err
	}

	var hashKey, enabledKey string
	if name == AdminUsername {
		hashKey, enabledKey = KeyAdminPassword, KeyAdminEnabled
	} else {
		hashKey = fmt.Sprintf("accounts.%s.password", name)
		enabledKey = fmt.Sprintf("accounts.%s.enabled", name)
	}

	hash := string(secret[hashKey])
	if hash == "" {
		return Account{}, fmt.Errorf("account %q not found", name)
	}
	return Account{
		Enabled:      parseBoolDefault(cm[enabledKey], true),
		PasswordHash: hash,
	}, nil
}

// VerifyUsernamePassword returns nil if username exists, is enabled, and
// password matches its stored hash. Any miss, disabled account, or mismatch
// returns a non-nil error (fail closed).
func (m *SettingsManager) VerifyUsernamePassword(ctx context.Context, username, pass string) error {
	account, err := m.GetAccount(ctx, username)
	if err != nil {
		return err
	}
	if !account.Enabled {
		return fmt.Errorf("account %q is disabled", username)
	}
	return password.VerifyPassword(pass, account.PasswordHash)
}
