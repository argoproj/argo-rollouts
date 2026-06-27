package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

func TestGenerateSelfSignedCert(t *testing.T) {
	cert, err := generateSelfSignedCert()
	require.NoError(t, err)
	require.NotEmpty(t, cert.Certificate)

	parsed, err := x509.ParseCertificate(cert.Certificate[0])
	require.NoError(t, err)
	assert.Equal(t, "argo-rollouts-dashboard", parsed.Subject.CommonName)
	assert.Contains(t, parsed.DNSNames, "localhost")
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
