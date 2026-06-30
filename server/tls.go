package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"net"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/argoproj/argo-rollouts/server/auth/settings"
)

// generateSelfSignedCert returns an in-memory ECDSA self-signed certificate
// valid for localhost, for use when no TLS material is supplied.
func generateSelfSignedCert() (tls.Certificate, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("generate key: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("generate serial: %w", err)
	}
	now := time.Now()
	tmpl := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "argo-rollouts-dashboard"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("create certificate: %w", err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: priv}, nil
}

// maybeTLSConfig returns the TLS config to serve with, or nil for plaintext.
// TLS is enabled in server mode unless --insecure is set.
func (s *ArgoRolloutsServer) maybeTLSConfig(ctx context.Context) (*tls.Config, error) {
	if s.Options.AuthMode != AuthModeServer || s.Options.Insecure {
		return nil, nil
	}
	return s.buildTLSConfig(ctx)
}

// buildTLSConfig returns a TLS config for the dashboard: the Secret-provided
// certificate if configured, otherwise a generated self-signed certificate.
func (s *ArgoRolloutsServer) buildTLSConfig(ctx context.Context) (*tls.Config, error) {
	sm := settings.NewSettingsManager(s.Options.KubeClientset, s.Options.Namespace)
	cert, ok, err := sm.GetTLSCertificate(ctx)
	if err != nil {
		return nil, err
	}
	if !ok {
		generated, genErr := generateSelfSignedCert()
		if genErr != nil {
			return nil, genErr
		}
		log.Info("no TLS certificate configured; using a generated self-signed certificate")
		cert = &generated
	}
	return &tls.Config{
		Certificates: []tls.Certificate{*cert},
		MinVersion:   tls.VersionTLS12,
		// Advertise HTTP/2 (h2) and HTTP/1.1 via ALPN. The grpc-gateway dials the
		// in-process gRPC server over this same TLS port and requires an h2 ALPN
		// selection; without this the handshake fails with "missing selected ALPN
		// property" and gateway calls surface as HTTP 503.
		NextProtos: []string{"h2", "http/1.1"},
	}, nil
}
