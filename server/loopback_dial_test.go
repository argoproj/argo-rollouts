package server

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// certWithSANs builds a self-signed certificate carrying the given DNS and IP
// SANs, returning a tls.Certificate with a populated Leaf for assertions.
func certWithSANs(t *testing.T, dns []string, ips []net.IP) tls.Certificate {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		DNSNames:     dns,
		IPAddresses:  ips,
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	require.NoError(t, err)
	leaf, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: priv, Leaf: leaf}
}

func TestLoopbackDialCreds(t *testing.T) {
	t.Run("prefers a DNS SAN as ServerName", func(t *testing.T) {
		cert := certWithSANs(t, []string{"localhost"}, []net.IP{net.IPv4(127, 0, 0, 1)})
		creds, err := loopbackDialCreds(&tls.Config{Certificates: []tls.Certificate{cert}})
		require.NoError(t, err)
		require.NotNil(t, creds)
		assert.Equal(t, "localhost", creds.Info().ServerName)
	})

	t.Run("falls back to an IP SAN when no DNS SAN", func(t *testing.T) {
		cert := certWithSANs(t, nil, []net.IP{net.IPv4(127, 0, 0, 1)})
		creds, err := loopbackDialCreds(&tls.Config{Certificates: []tls.Certificate{cert}})
		require.NoError(t, err)
		assert.Equal(t, "127.0.0.1", creds.Info().ServerName)
	})

	t.Run("falls back to connectAddr when no SANs", func(t *testing.T) {
		cert := certWithSANs(t, nil, nil)
		creds, err := loopbackDialCreds(&tls.Config{Certificates: []tls.Certificate{cert}})
		require.NoError(t, err)
		assert.Equal(t, connectAddr, creds.Info().ServerName)
	})

	t.Run("parses the leaf when not pre-populated", func(t *testing.T) {
		cert := certWithSANs(t, []string{"localhost"}, nil)
		cert.Leaf = nil // force the x509.ParseCertificate path
		creds, err := loopbackDialCreds(&tls.Config{Certificates: []tls.Certificate{cert}})
		require.NoError(t, err)
		assert.Equal(t, "localhost", creds.Info().ServerName)
	})

	t.Run("errors when no certificate is configured", func(t *testing.T) {
		_, err := loopbackDialCreds(&tls.Config{})
		require.Error(t, err)
	})
}
