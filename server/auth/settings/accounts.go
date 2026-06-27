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

// dummyPasswordHash is a precomputed bcrypt hash used to equalise timing on
// the not-found and disabled paths so they cost the same as a real comparison.
var dummyPasswordHash string

func init() {
	h, err := password.HashPassword("argo-rollouts-settings-timing-equalizer")
	if err != nil {
		panic("settings: failed to compute timing-equalizer hash: " + err.Error())
	}
	dummyPasswordHash = h
}

// VerifyUsernamePassword returns nil if username exists, is enabled, and
// password matches its stored hash. Any miss, disabled account, or mismatch
// returns a non-nil error (fail closed).
//
// Exactly one bcrypt comparison runs on every code path to prevent
// credential-timing enumeration (unknown-user and disabled-account paths
// both perform a dummy comparison against dummyPasswordHash).
func (m *SettingsManager) VerifyUsernamePassword(ctx context.Context, username, pass string) error {
	account, err := m.GetAccount(ctx, username)
	if err != nil {
		// Unknown account: burn one bcrypt to equalise timing with the real path.
		_ = password.VerifyPassword(pass, dummyPasswordHash)
		return err
	}
	if !account.Enabled {
		// Disabled account: burn one bcrypt to equalise timing with the real path.
		_ = password.VerifyPassword(pass, dummyPasswordHash)
		return fmt.Errorf("account %q is disabled", username)
	}
	return password.VerifyPassword(pass, account.PasswordHash)
}
