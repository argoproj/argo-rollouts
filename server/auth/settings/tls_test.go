package settings

import (
	"context"
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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// testCertPEM returns a throwaway self-signed cert+key as PEM, for seeding the Secret.
func testCertPEM(t *testing.T) (certPEM, keyPEM []byte) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	require.NoError(t, err)
	keyDER, err := x509.MarshalECPrivateKey(priv)
	require.NoError(t, err)
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM
}

func TestGetTLSCertificateConfigured(t *testing.T) {
	certPEM, keyPEM := testCertPEM(t)
	client := fake.NewSimpleClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: SecretName, Namespace: testNamespace},
		Data:       map[string][]byte{KeyTLSCert: certPEM, KeyTLSKey: keyPEM},
	})
	m := NewSettingsManager(client, testNamespace)

	cert, ok, err := m.GetTLSCertificate(context.Background())
	require.NoError(t, err)
	assert.True(t, ok)
	require.NotNil(t, cert)
	assert.NotEmpty(t, cert.Certificate)
}

func TestGetTLSCertificateNotConfigured(t *testing.T) {
	client := fake.NewSimpleClientset() // no secret
	m := NewSettingsManager(client, testNamespace)

	cert, ok, err := m.GetTLSCertificate(context.Background())
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Nil(t, cert)
}

func TestGetTLSCertificateInvalid(t *testing.T) {
	client := fake.NewSimpleClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: SecretName, Namespace: testNamespace},
		Data:       map[string][]byte{KeyTLSCert: []byte("not-a-cert"), KeyTLSKey: []byte("not-a-key")},
	})
	m := NewSettingsManager(client, testNamespace)

	_, ok, err := m.GetTLSCertificate(context.Background())
	assert.Error(t, err, "present-but-invalid keypair must error, not silently self-sign")
	assert.False(t, ok)
}
