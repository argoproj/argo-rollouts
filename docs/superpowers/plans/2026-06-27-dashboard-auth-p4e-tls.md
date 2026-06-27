# Dashboard Auth — Plan 4e: TLS Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Serve the dashboard over TLS in server mode — load a cert/key from the dashboard Secret or generate a self-signed cert, wrap the listener so browser traffic is encrypted, make the in-process gateway dial TLS-aware, and mark the session cookie `Secure` — with a `--insecure` opt-out for deployments that terminate TLS at an ingress.

**Architecture:** Two new units plus wiring. `settings.GetTLSCertificate` loads `tls.crt`/`tls.key` from the dashboard Secret (returns "not configured" cleanly when absent). `server/tls.go` provides `generateSelfSignedCert` (a standard ECDSA self-signed cert valid for localhost) and `buildTLSConfig`, which prefers the Secret-provided cert and otherwise self-signs. `Run` wraps the cmux listener with `tls.NewListener` when a TLS config is present; `newHTTPServer`'s in-process gateway dial switches from `grpc.WithInsecure()` to TLS transport credentials (loopback, `InsecureSkipVerify`); and `setupAuth` sets `LoginHandler.Secure = true` under TLS. TLS is enabled automatically in server mode unless `--insecure` is set; `none` mode is untouched (plaintext, unchanged).

**Tech Stack:** Go `crypto/tls`, `crypto/x509`, `crypto/ecdsa`, `crypto/elliptic`, `crypto/rand`, `encoding/pem`, `math/big`, `google.golang.org/grpc/credentials`, the `settings`/`server` packages, testify.

## Global Constraints

- Module `github.com/argoproj/argo-rollouts`. Files: `server/auth/settings/tls.go` (new) + test; `server/tls.go` (new) + test; `server/server.go`, `server/auth_setup.go`, `pkg/kubectl-argo-rollouts/cmd/dashboard/dashboard.go` (edits).
- TLS applies ONLY in server mode (`AuthMode == AuthModeServer`) and only when NOT `--insecure`. `none` mode stays plaintext, byte-identical to today. The `--insecure` flag lets server mode run plaintext (ingress-terminated TLS).
- Cert source precedence: the dashboard Secret's `tls.crt`+`tls.key` if BOTH present and valid; otherwise an in-memory self-signed cert (logged as such). A present-but-invalid keypair is an error (fail loud), not a silent self-sign.
- Self-signed cert: ECDSA P-256, CN `argo-rollouts-dashboard`, SANs `localhost` + `127.0.0.1` + `::1`, validity ~10 years, `ExtKeyUsageServerAuth`.
- The in-process gateway dial (gateway → local gRPC) uses TLS credentials with `InsecureSkipVerify: true` (loopback, self-signed-friendly) when TLS is on; it stays `grpc.WithInsecure()` when TLS is off. Never leave a plaintext dial against a TLS listener (handshake would fail).
- `LoginHandler.Secure` MUST be `true` whenever TLS is active and `false` otherwise (so the cookie is sent only over HTTPS under TLS, but still works in plaintext `--insecure`/none).
- Reuse `settings.SettingsManager.secretData` pattern (the package already reads the Secret); add the TLS keys as exported consts.
- testify; tests generate throwaway certs with the standard library.

---

### Task 1: Load TLS certificate from the Secret

**Files:**
- Create: `server/auth/settings/tls.go`
- Test: `server/auth/settings/tls_test.go`

**Interfaces:**
- Consumes: `SettingsManager.secretData`.
- Produces:
  - `const KeyTLSCert = "tls.crt"`, `const KeyTLSKey = "tls.key"`
  - `func (m *SettingsManager) GetTLSCertificate(ctx context.Context) (*tls.Certificate, bool, error)` — `(cert, true, nil)` if both keys present and valid; `(nil, false, nil)` if not configured; `(nil, false, err)` if present but invalid.

- [ ] **Step 1: Write the failing test**

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./server/auth/settings/ -run TestGetTLSCertificate -v`
Expected: FAIL — `undefined: (*SettingsManager).GetTLSCertificate`, `undefined: KeyTLSCert`.

- [ ] **Step 3: Write minimal implementation**

```go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./server/auth/settings/ -run TestGetTLSCertificate -v`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git add server/auth/settings/tls.go server/auth/settings/tls_test.go
git commit -m "feat(settings): load dashboard TLS certificate from secret"
```

---

### Task 2: Self-signed cert + TLS config builder

**Files:**
- Create: `server/tls.go`
- Test: `server/tls_test.go`

**Interfaces:**
- Consumes: `settings.SettingsManager.GetTLSCertificate`.
- Produces:
  - `func generateSelfSignedCert() (tls.Certificate, error)`
  - `func (s *ArgoRolloutsServer) buildTLSConfig(ctx context.Context) (*tls.Config, error)` — prefers the Secret cert, else self-signs; returns a `*tls.Config` with the chosen certificate.

- [ ] **Step 1: Write the failing test**

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./server/ -run 'TestGenerateSelfSignedCert|TestBuildTLSConfig' -v`
Expected: FAIL — `undefined: generateSelfSignedCert`, `undefined: (*ArgoRolloutsServer).buildTLSConfig`.

- [ ] **Step 3: Write minimal implementation**

```go
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
	}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./server/ -run 'TestGenerateSelfSignedCert|TestBuildTLSConfig' -v`
Expected: PASS (2 tests).

- [ ] **Step 5: Commit**

```bash
git add server/tls.go server/tls_test.go
git commit -m "feat(server): self-signed cert and TLS config builder"
```

---

### Task 3: Wire TLS into the server + --insecure flag

**Files:**
- Modify: `server/server.go` (ArgoRolloutsServer struct, Run, newHTTPServer)
- Modify: `server/auth_setup.go` (LoginHandler.Secure)
- Modify: `pkg/kubectl-argo-rollouts/cmd/dashboard/dashboard.go` (--insecure flag, ServerOptions.Insecure)
- Test: `server/tls_test.go` (add wiring tests)

**Interfaces:**
- Consumes: `buildTLSConfig` (Task 2).
- Produces:
  - `ServerOptions.Insecure bool`; `--insecure` flag (default false).
  - `ArgoRolloutsServer.tlsConfig *tls.Config` (nil = plaintext).
  - `Run` builds `s.tlsConfig` (server mode && !Insecure) and wraps the listener with `tls.NewListener`.
  - `newHTTPServer` dials the in-process gateway with TLS credentials when `s.tlsConfig != nil`.
  - `setupAuth` sets `LoginHandler.Secure = (s.tlsConfig != nil)`.

- [ ] **Step 1: Write the failing test**

```go
func TestRunBuildsTLSConfigInServerMode(t *testing.T) {
	client := k8sfake.NewSimpleClientset()
	s := NewServer(ServerOptions{KubeClientset: client, Namespace: "argo-rollouts", AuthMode: AuthModeServer})

	// Exercise just the TLS-config decision used by Run, not the full listener.
	cfg, err := s.maybeTLSConfig(context.Background())
	require.NoError(t, err)
	assert.NotNil(t, cfg, "server mode without --insecure must enable TLS")
}

func TestMaybeTLSConfigDisabled(t *testing.T) {
	client := k8sfake.NewSimpleClientset()

	none := NewServer(ServerOptions{KubeClientset: client, Namespace: "argo-rollouts", AuthMode: AuthModeNone})
	cfg, err := none.maybeTLSConfig(context.Background())
	require.NoError(t, err)
	assert.Nil(t, cfg, "none mode stays plaintext")

	insecure := NewServer(ServerOptions{KubeClientset: client, Namespace: "argo-rollouts", AuthMode: AuthModeServer, Insecure: true})
	cfg, err = insecure.maybeTLSConfig(context.Background())
	require.NoError(t, err)
	assert.Nil(t, cfg, "--insecure disables TLS even in server mode")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./server/ -run 'TestRunBuildsTLSConfig|TestMaybeTLSConfig' -v`
Expected: FAIL — `undefined: (*ArgoRolloutsServer).maybeTLSConfig`, `unknown field Insecure`.

- [ ] **Step 3: Implement**

Add the field to `ServerOptions` (after `AuthMode`):

```go
	AuthMode          string
	Insecure          bool
```

Add to `ArgoRolloutsServer` struct:

```go
type ArgoRolloutsServer struct {
	Options   ServerOptions
	stopCh    chan struct{}
	auth      *authComponents
	tlsConfig *tls.Config
}
```

(Add `"crypto/tls"` to `server.go` imports.)

Add `maybeTLSConfig` to `server/tls.go` (the seam the test drives, and what `Run` calls):

```go
// maybeTLSConfig returns the TLS config to serve with, or nil for plaintext.
// TLS is enabled in server mode unless --insecure is set.
func (s *ArgoRolloutsServer) maybeTLSConfig(ctx context.Context) (*tls.Config, error) {
	if s.Options.AuthMode != AuthModeServer || s.Options.Insecure {
		return nil, nil
	}
	return s.buildTLSConfig(ctx)
}
```

In `Run`, after the existing auth setup block and BEFORE `newHTTPServer`/`newGRPCServer`, build the TLS config and store it:

```go
	tlsConfig, err := s.maybeTLSConfig(ctx)
	errors.CheckError(err)
	s.tlsConfig = tlsConfig
```

In `Run`, after the listener is created (`conn, realErr = net.Listen(...)` succeeds, i.e. just before `tcpm := cmux.New(conn)`), wrap it when TLS is on:

```go
	if s.tlsConfig != nil {
		conn = tls.NewListener(conn, s.tlsConfig)
	}
	tcpm := cmux.New(conn)
```

Update the startup message scheme to `https` when TLS is on (optional but correct):

```go
	scheme := "http"
	if s.tlsConfig != nil {
		scheme = "https"
	}
	// in the dashboard startupMessage, use scheme instead of the literal "http"
```

In `newHTTPServer`, make the gateway dial TLS-aware — replace the `opts = append(opts, grpc.WithInsecure())` line:

```go
	if s.tlsConfig != nil {
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{InsecureSkipVerify: true})))
	} else {
		opts = append(opts, grpc.WithInsecure())
	}
```

(Add `"google.golang.org/grpc/credentials"` and `"crypto/tls"` to `server.go` imports. The loopback in-process dial uses `InsecureSkipVerify` because the cert is self-signed and the connection never leaves the host.)

In `server/auth_setup.go` `setupAuth`, set the login cookie Secure flag from TLS state:

```go
		login: &auth.LoginHandler{Verifier: sm, Issuer: sessionMgr, TokenExpiry: tokenExpiry, Secure: s.tlsConfig != nil},
```

(Note: `setupAuth` runs in `Run` BEFORE `maybeTLSConfig` in the order shown above — reorder so `maybeTLSConfig`/`s.tlsConfig` is set BEFORE `setupAuth`, so the login handler sees the right `Secure` value. In `Run`, build `s.tlsConfig` first, then call `setupAuth`.)

In `dashboard.go`, add the flag:

```go
	var insecure bool
	// in the ServerOptions literal:
		Insecure: insecure,
	// with the other flags:
	cmd.Flags().BoolVar(&insecure, "insecure", false, "disable TLS in server mode (e.g. when TLS is terminated upstream)")
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./server/ -run 'TestRunBuildsTLSConfig|TestMaybeTLSConfig' -v`
Expected: PASS (2 tests).

- [ ] **Step 5: Build + full regression**

Run: `go build ./... && go test ./server/... && go vet ./server/...`
Expected: module builds; existing `server` + auth tests pass (none-mode untouched: `maybeTLSConfig` returns nil, gateway dial stays `WithInsecure`, listener unwrapped).

- [ ] **Step 6: Commit**

```bash
git add server/server.go server/tls.go server/auth_setup.go pkg/kubectl-argo-rollouts/cmd/dashboard/
git commit -m "feat(server): serve TLS in server mode with --insecure opt-out"
```

---

## Self-Review

**Spec coverage (vs design §9 security):**
- TLS on by default in server mode (self-signed if no cert) → Tasks 2 + 3. ✅
- Cert/key from the dashboard Secret → Task 1. ✅
- `grpc.WithInsecure()` replaced under TLS (gateway dial) → Task 3. ✅
- Session cookie `Secure` under TLS → Task 3 (`setupAuth` reads `s.tlsConfig`). ✅
- `--insecure` opt-out for ingress-terminated TLS → Task 3. ✅
- `none` mode unchanged (plaintext) → `maybeTLSConfig` returns nil for non-server / insecure. ✅

**Placeholder scan:** No TBD/TODO; each step gives complete code and the exact insertion site. ✅

**Type consistency:** `GetTLSCertificate`, `KeyTLSCert/KeyTLSKey`, `generateSelfSignedCert`, `buildTLSConfig`, `maybeTLSConfig`, `ServerOptions.Insecure`, `ArgoRolloutsServer.tlsConfig` consistent across Tasks 1–3. ✅

**Ordering note (load-bearing):** In `Run`, `s.tlsConfig` MUST be set before `setupAuth`, so `LoginHandler.Secure` reflects TLS. The plan sequences them accordingly.

**Security notes:**
- A present-but-invalid keypair errors loudly (Task 1) rather than silently downgrading to self-signed.
- The in-process gateway dial's `InsecureSkipVerify` is scoped to loopback (gateway → local gRPC on the same host); external clients still validate against the served cert. Documented inline.
- Self-signed default means browsers warn until a real cert is supplied via the Secret — expected, matches argo-cd; document in the manifests/ops notes (Plan 7).

**Carried forward:**
- Cert hot-reload on Secret change (currently read once at startup) — future, alongside the settings watch already deferred from P4d.
- mTLS / client-cert auth — out of scope.
- TLS min-version is set to 1.2 in `buildTLSConfig`. HSTS response header — a reasonable follow-up, not included here.
