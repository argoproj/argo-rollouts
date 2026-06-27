package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"

	"github.com/argoproj/argo-rollouts/server/auth/settings"
)

// genCertPEM generates a throwaway ECDSA P-256 self-signed certificate with the
// given CommonName and returns PEM-encoded cert and key.
func genCertPEM(t *testing.T, cn string) (certPEM, keyPEM []byte) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	require.NoError(t, err)
	keyDER, err := x509.MarshalECPrivateKey(priv)
	require.NoError(t, err)
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM
}

func TestGenerateSelfSignedCert(t *testing.T) {
	cert, err := generateSelfSignedCert()
	require.NoError(t, err)
	require.NotEmpty(t, cert.Certificate)

	parsed, err := x509.ParseCertificate(cert.Certificate[0])
	require.NoError(t, err)
	assert.Equal(t, "argo-rollouts-dashboard", parsed.Subject.CommonName)
	assert.Contains(t, parsed.DNSNames, "localhost")

	// Fix 3: verify IP SANs and extended key usage
	foundIPv4 := false
	foundIPv6 := false
	for _, ip := range parsed.IPAddresses {
		if ip.Equal(net.ParseIP("127.0.0.1")) {
			foundIPv4 = true
		}
		if ip.Equal(net.IPv6loopback) {
			foundIPv6 = true
		}
	}
	assert.True(t, foundIPv4, "IPAddresses should contain 127.0.0.1")
	assert.True(t, foundIPv6, "IPAddresses should contain ::1")
	assert.Contains(t, parsed.ExtKeyUsage, x509.ExtKeyUsageServerAuth)
}

func TestBuildTLSConfigSelfSignsWhenNoSecret(t *testing.T) {
	client := k8sfake.NewSimpleClientset() // no secret => self-sign
	s := NewServer(ServerOptions{KubeClientset: client, Namespace: "argo-rollouts"})

	cfg, err := s.buildTLSConfig(context.Background())
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Len(t, cfg.Certificates, 1, "exactly one certificate configured")
}

func TestBuildTLSConfigMinVersion(t *testing.T) {
	client := k8sfake.NewSimpleClientset()
	s := NewServer(ServerOptions{KubeClientset: client, Namespace: "argo-rollouts"})

	cfg, err := s.buildTLSConfig(context.Background())
	require.NoError(t, err)
	assert.Equal(t, uint16(tls.VersionTLS12), cfg.MinVersion)
}

// TestBuildTLSConfigPropagatesCertError verifies that buildTLSConfig propagates
// errors from GetTLSCertificate rather than silently falling back to self-signed.
func TestBuildTLSConfigPropagatesCertError(t *testing.T) {
	client := k8sfake.NewSimpleClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: settings.SecretName, Namespace: "argo-rollouts"},
		Data: map[string][]byte{
			settings.KeyTLSCert: []byte("bad"),
			settings.KeyTLSKey:  []byte("bad"),
		},
	})
	s := NewServer(ServerOptions{KubeClientset: client, Namespace: "argo-rollouts"})
	cfg, err := s.buildTLSConfig(context.Background())
	assert.Error(t, err)
	assert.Nil(t, cfg)
}

// TestBuildTLSConfigUsesConfiguredCert verifies that when a valid TLS keypair is
// stored in the Secret, buildTLSConfig uses it (not a self-signed fallback).
func TestBuildTLSConfigUsesConfiguredCert(t *testing.T) {
	const cn = "configured-cert.example"
	certPEM, keyPEM := genCertPEM(t, cn)
	client := k8sfake.NewSimpleClientset(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: settings.SecretName, Namespace: "argo-rollouts"},
		Data: map[string][]byte{
			settings.KeyTLSCert: certPEM,
			settings.KeyTLSKey:  keyPEM,
		},
	})
	s := NewServer(ServerOptions{KubeClientset: client, Namespace: "argo-rollouts"})
	cfg, err := s.buildTLSConfig(context.Background())
	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.Len(t, cfg.Certificates, 1)

	parsed, err := x509.ParseCertificate(cfg.Certificates[0].Certificate[0])
	require.NoError(t, err)
	assert.Equal(t, cn, parsed.Subject.CommonName,
		"cfg must use the configured cert, not the self-signed argo-rollouts-dashboard cert")
}
