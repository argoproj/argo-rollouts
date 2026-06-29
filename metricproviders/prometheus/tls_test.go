package prometheus

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

// generateTestCACert returns a PEM-encoded self-signed CA certificate usable in tests.
func generateTestCACert(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "argo-rollouts-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	require.NoError(t, err)

	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	require.NotNil(t, pemBytes)
	return string(pemBytes)
}

// TestNewCACertTransportValid verifies that a custom CA bundle is wired into the
// transport's RootCAs while leaving verification enabled by default.
func TestNewCACertTransportValid(t *testing.T) {
	caCert := generateTestCACert(t)

	transport, err := newCACertTransport(caCert, false)
	require.NoError(t, err)
	require.NotNil(t, transport)
	require.NotNil(t, transport.TLSClientConfig)
	assert.NotNil(t, transport.TLSClientConfig.RootCAs)
	assert.False(t, transport.TLSClientConfig.InsecureSkipVerify)

	// A custom-CA transport must not reuse the shared singletons.
	assert.NotSame(t, secureTransport, transport)
	assert.NotSame(t, insecureTransport, transport)
}

// TestNewCACertTransportInsecure verifies insecure: true is still honored when a
// CA cert is supplied.
func TestNewCACertTransportInsecure(t *testing.T) {
	caCert := generateTestCACert(t)

	transport, err := newCACertTransport(caCert, true)
	require.NoError(t, err)
	require.NotNil(t, transport.TLSClientConfig)
	assert.True(t, transport.TLSClientConfig.InsecureSkipVerify)
	assert.NotNil(t, transport.TLSClientConfig.RootCAs)
}

// TestNewCACertTransportInvalid verifies that a non-PEM caCert produces an error
// rather than silently falling back to no verification.
func TestNewCACertTransportInvalid(t *testing.T) {
	transport, err := newCACertTransport("not-a-valid-pem-certificate", false)
	assert.Error(t, err)
	assert.Nil(t, transport)
}

// TestNewPrometheusAPIWithCACert exercises the end-to-end client construction
// with a custom CA cert (valid and invalid).
func TestNewPrometheusAPIWithCACert(t *testing.T) {
	caCert := generateTestCACert(t)

	metric := v1alpha1.Metric{
		Provider: v1alpha1.MetricProvider{
			Prometheus: &v1alpha1.PrometheusMetric{
				Address: "https://prometheus.example.com:9090",
				CACert:  caCert,
			},
		},
	}
	api, err := NewPrometheusAPI(metric)
	assert.NoError(t, err)
	assert.NotNil(t, api)

	// Invalid PEM should bubble up an error.
	metric.Provider.Prometheus.CACert = "garbage"
	_, err = NewPrometheusAPI(metric)
	assert.Error(t, err)
}

// TestNewPrometheusAPIDefaultTLS verifies that with no TLS config the shared
// secure transport is used, preserving existing behavior.
func TestNewPrometheusAPIDefaultTLS(t *testing.T) {
	metric := v1alpha1.Metric{
		Provider: v1alpha1.MetricProvider{
			Prometheus: &v1alpha1.PrometheusMetric{
				Address: "https://prometheus.example.com:9090",
			},
		},
	}
	api, err := NewPrometheusAPI(metric)
	assert.NoError(t, err)
	assert.NotNil(t, api)
	// Default (no CACert, no insecure) must keep verification enabled.
	assert.False(t, secureTransport.TLSClientConfig.InsecureSkipVerify)
}
