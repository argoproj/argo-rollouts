package settings

import (
	"context"
	"crypto/tls"
	"fmt"
)

// TLS Secret data keys.
const (
	KeyTLSCert = "tls.crt"
	KeyTLSKey  = "tls.key"
)

// GetTLSCertificate loads the dashboard's TLS keypair from the Secret. It
// returns (cert, true, nil) when both tls.crt and tls.key are present and form
// a valid keypair; (nil, false, nil) when TLS is not configured; and an error
// when the material is present but invalid.
func (m *SettingsManager) GetTLSCertificate(ctx context.Context) (*tls.Certificate, bool, error) {
	data, err := m.secretData(ctx)
	if err != nil {
		return nil, false, err
	}
	crt := data[KeyTLSCert]
	key := data[KeyTLSKey]
	if len(crt) == 0 || len(key) == 0 {
		return nil, false, nil
	}
	cert, err := tls.X509KeyPair(crt, key)
	if err != nil {
		return nil, false, fmt.Errorf("invalid TLS keypair in secret: %w", err)
	}
	return &cert, true, nil
}
