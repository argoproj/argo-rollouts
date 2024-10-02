package session

import (
	"context"
	"encoding/pem"
	stderrors "errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/argoproj/argo-rollouts/common"
	"github.com/argoproj/argo-rollouts/utils/settings"
	utiltest "github.com/argoproj/argo-rollouts/utils/test"
)

func newSessionManager(settingsMgr *settings.SettingsManager, storage UserStateStorage) *SessionManager {
	mgr := NewSessionManager(settingsMgr, "", nil, storage)
	mgr.verificationDelayNoiseEnabled = false
	return mgr
}

type claimsMock struct {
	err error
}

func (cm *claimsMock) Valid() error {
	return cm.err
}

type tokenVerifierMock struct {
	claims *claimsMock
	err    error
}

func (tm *tokenVerifierMock) VerifyToken(token string) (jwt.Claims, string, error) {
	if tm.claims == nil {
		return nil, "", tm.err
	}
	return tm.claims, "", tm.err
}

func strPointer(str string) *string {
	return &str
}

func TestSessionManager_WithAuthMiddleware(t *testing.T) {
	handlerFunc := func() func(http.ResponseWriter, *http.Request) {
		return func(w http.ResponseWriter, r *http.Request) {
			t.Helper()
			w.WriteHeader(http.StatusOK)
			w.Header().Set("Content-Type", "application/text")
			_, err := w.Write([]byte("Ok"))
			if err != nil {
				t.Fatalf("error writing response: %s", err)
			}
		}
	}
	type testCase struct {
		name                 string
		authDisabled         bool
		cookieHeader         bool
		verifiedClaims       *claimsMock
		verifyTokenErr       error
		expectedStatusCode   int
		expectedResponseBody *string
	}

	cases := []testCase{
		{
			name:                 "will authenticate successfully",
			authDisabled:         false,
			cookieHeader:         true,
			verifiedClaims:       &claimsMock{},
			verifyTokenErr:       nil,
			expectedStatusCode:   200,
			expectedResponseBody: strPointer("Ok"),
		},
		{
			name:                 "will be noop if auth is disabled",
			authDisabled:         true,
			cookieHeader:         false,
			verifiedClaims:       nil,
			verifyTokenErr:       nil,
			expectedStatusCode:   200,
			expectedResponseBody: strPointer("Ok"),
		},
		{
			name:                 "will return 400 if no cookie header",
			authDisabled:         false,
			cookieHeader:         false,
			verifiedClaims:       &claimsMock{},
			verifyTokenErr:       nil,
			expectedStatusCode:   400,
			expectedResponseBody: nil,
		},
		{
			name:                 "will return 401 verify token fails",
			authDisabled:         false,
			cookieHeader:         true,
			verifiedClaims:       &claimsMock{},
			verifyTokenErr:       stderrors.New("token error"),
			expectedStatusCode:   401,
			expectedResponseBody: nil,
		},
		{
			name:                 "will return 200 if claims are nil",
			authDisabled:         false,
			cookieHeader:         true,
			verifiedClaims:       nil,
			verifyTokenErr:       nil,
			expectedStatusCode:   200,
			expectedResponseBody: strPointer("Ok"),
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// given
			mux := http.NewServeMux()
			mux.HandleFunc("/", handlerFunc())
			tm := &tokenVerifierMock{
				claims: tc.verifiedClaims,
				err:    tc.verifyTokenErr,
			}
			ts := httptest.NewServer(WithAuthMiddleware(tc.authDisabled, tm, mux))
			defer ts.Close()
			req, err := http.NewRequest(http.MethodGet, ts.URL, nil)
			if err != nil {
				t.Fatalf("error creating request: %s", err)
			}
			if tc.cookieHeader {
				req.Header.Add("Cookie", "argocd.token=123456")
			}

			// when
			resp, err := http.DefaultClient.Do(req)

			// then
			require.NoError(t, err)
			assert.NotNil(t, resp)
			assert.Equal(t, tc.expectedStatusCode, resp.StatusCode)
			if tc.expectedResponseBody != nil {
				body, err := io.ReadAll(resp.Body)
				require.NoError(t, err)
				actual := strings.TrimSuffix(string(body), "\n")
				assert.Contains(t, actual, *tc.expectedResponseBody)
			}
		})
	}
}

var loggedOutContext = context.Background()

// nolint:staticcheck
var loggedInContext = context.WithValue(context.Background(), "claims", &jwt.MapClaims{"iss": "qux", "sub": "foo", "email": "bar", "groups": []string{"baz"}})

func TestIss(t *testing.T) {
	assert.Empty(t, Iss(loggedOutContext))
	assert.Equal(t, "qux", Iss(loggedInContext))
}

func TestLoggedIn(t *testing.T) {
	assert.False(t, LoggedIn(loggedOutContext))
	assert.True(t, LoggedIn(loggedInContext))
}

func TestUsername(t *testing.T) {
	assert.Empty(t, Username(loggedOutContext))
	assert.Equal(t, "bar", Username(loggedInContext))
}

func TestSub(t *testing.T) {
	assert.Empty(t, Sub(loggedOutContext))
	assert.Equal(t, "foo", Sub(loggedInContext))
}

func TestGroups(t *testing.T) {
	assert.Empty(t, Groups(loggedOutContext, []string{"groups"}))
	assert.Equal(t, []string{"baz"}, Groups(loggedInContext, []string{"groups"}))
}

func TestCacheValueGetters(t *testing.T) {
	t.Run("Default values", func(t *testing.T) {
		mlf := getMaxLoginFailures()
		assert.Equal(t, defaultMaxLoginFailures, mlf)

		mcs := getMaximumCacheSize()
		assert.Equal(t, defaultMaxCacheSize, mcs)
	})

	t.Run("Valid environment overrides", func(t *testing.T) {
		t.Setenv(envLoginMaxFailCount, "5")
		t.Setenv(envLoginMaxCacheSize, "5")

		mlf := getMaxLoginFailures()
		assert.Equal(t, 5, mlf)

		mcs := getMaximumCacheSize()
		assert.Equal(t, 5, mcs)
	})

	t.Run("Invalid environment overrides", func(t *testing.T) {
		t.Setenv(envLoginMaxFailCount, "invalid")
		t.Setenv(envLoginMaxCacheSize, "invalid")

		mlf := getMaxLoginFailures()
		assert.Equal(t, defaultMaxLoginFailures, mlf)

		mcs := getMaximumCacheSize()
		assert.Equal(t, defaultMaxCacheSize, mcs)
	})

	t.Run("Less than allowed in environment overrides", func(t *testing.T) {
		t.Setenv(envLoginMaxFailCount, "-1")
		t.Setenv(envLoginMaxCacheSize, "-1")

		mlf := getMaxLoginFailures()
		assert.Equal(t, defaultMaxLoginFailures, mlf)

		mcs := getMaximumCacheSize()
		assert.Equal(t, defaultMaxCacheSize, mcs)
	})

	t.Run("Greater than allowed in environment overrides", func(t *testing.T) {
		t.Setenv(envLoginMaxFailCount, fmt.Sprintf("%d", math.MaxInt32+1))
		t.Setenv(envLoginMaxCacheSize, fmt.Sprintf("%d", math.MaxInt32+1))

		mlf := getMaxLoginFailures()
		assert.Equal(t, defaultMaxLoginFailures, mlf)

		mcs := getMaximumCacheSize()
		assert.Equal(t, defaultMaxCacheSize, mcs)
	})
}

func getKubeClientWithConfig(config map[string]string, secretConfig map[string][]byte) *fake.Clientset {
	mergedSecretConfig := map[string][]byte{
		"server.secretkey": []byte("Hello, world!"),
	}
	for key, value := range secretConfig {
		mergedSecretConfig[key] = value
	}

	return fake.NewSimpleClientset(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "argocd-cm",
			Namespace: "argocd",
			Labels: map[string]string{
				"app.kubernetes.io/part-of": "argo-rollouts",
			},
		},
		Data: config,
	}, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "argocd-secret",
			Namespace: "argocd",
		},
		Data: mergedSecretConfig,
	})
}

func TestSessionManager_VerifyToken(t *testing.T) {
	oidcTestServer := utiltest.GetOIDCTestServer(t)
	t.Cleanup(oidcTestServer.Close)

	dexTestServer := utiltest.GetDexTestServer(t)
	t.Cleanup(dexTestServer.Close)

	t.Run("RS512 is supported", func(t *testing.T) {
		dexConfig := map[string]string{
			"url": "",
			"oidc.config": fmt.Sprintf(`
name: Test
issuer: %s
clientID: xxx
clientSecret: yyy
requestedScopes: ["oidc"]`, oidcTestServer.URL),
		}

		settingsMgr := settings.NewSettingsManager(context.Background(), getKubeClientWithConfig(dexConfig, nil), "argocd")
		mgr := NewSessionManager(settingsMgr, "", nil, NewUserStateStorage(nil))
		mgr.verificationDelayNoiseEnabled = false
		// Use test server's client to avoid TLS issues.
		mgr.client = oidcTestServer.Client()

		claims := jwt.RegisteredClaims{Audience: jwt.ClaimStrings{"test-client"}, Subject: "admin", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour * 24))}
		claims.Issuer = oidcTestServer.URL
		token := jwt.NewWithClaims(jwt.SigningMethodRS512, claims)
		key, err := jwt.ParseRSAPrivateKeyFromPEM(utiltest.PrivateKey)
		require.NoError(t, err)
		tokenString, err := token.SignedString(key)
		require.NoError(t, err)

		_, _, err = mgr.VerifyToken(tokenString)
		assert.NotContains(t, err.Error(), "oidc: id token signed with unsupported algorithm")
	})

	t.Run("oidcConfig.rootCA is respected", func(t *testing.T) {
		cert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: oidcTestServer.TLS.Certificates[0].Certificate[0]})

		dexConfig := map[string]string{
			"url": "",
			"oidc.config": fmt.Sprintf(`
name: Test
issuer: %s
clientID: xxx
clientSecret: yyy
requestedScopes: ["oidc"]
rootCA: |
  %s
`, oidcTestServer.URL, strings.ReplaceAll(string(cert), "\n", "\n  ")),
		}

		settingsMgr := settings.NewSettingsManager(context.Background(), getKubeClientWithConfig(dexConfig, nil), "argocd")
		mgr := NewSessionManager(settingsMgr, "", nil, NewUserStateStorage(nil))
		mgr.verificationDelayNoiseEnabled = false

		claims := jwt.RegisteredClaims{Audience: jwt.ClaimStrings{"test-client"}, Subject: "admin", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour * 24))}
		claims.Issuer = oidcTestServer.URL
		token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
		key, err := jwt.ParseRSAPrivateKeyFromPEM(utiltest.PrivateKey)
		require.NoError(t, err)
		tokenString, err := token.SignedString(key)
		require.NoError(t, err)

		_, _, err = mgr.VerifyToken(tokenString)
		// If the root CA is being respected, we won't get this error. The error message is environment-dependent, so
		// we check for either of the error messages associated with a failed cert check.
		assert.NotContains(t, err.Error(), "certificate is not trusted")
		assert.NotContains(t, err.Error(), "certificate signed by unknown authority")
	})

	t.Run("OIDC provider is Dex, TLS is configured", func(t *testing.T) {
		dexConfig := map[string]string{
			"url": dexTestServer.URL,
			"dex.config": `connectors:
- type: github
  name: GitHub
  config:
    clientID: aabbccddeeff00112233
    clientSecret: aabbccddeeff00112233`,
		}

		// This is not actually used in the test. The test only calls the OIDC test server. But a valid cert/key pair
		// must be set to test VerifyToken's behavior when Argo CD is configured with TLS enabled.
		secretConfig := map[string][]byte{
			"tls.crt": utiltest.Cert,
			"tls.key": utiltest.PrivateKey,
		}

		settingsMgr := settings.NewSettingsManager(context.Background(), getKubeClientWithConfig(dexConfig, secretConfig), "argocd")
		mgr := NewSessionManager(settingsMgr, dexTestServer.URL, nil, NewUserStateStorage(nil))
		mgr.verificationDelayNoiseEnabled = false

		claims := jwt.RegisteredClaims{Audience: jwt.ClaimStrings{"test-client"}, Subject: "admin", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour * 24))}
		claims.Issuer = fmt.Sprintf("%s/api/dex", dexTestServer.URL)
		token := jwt.NewWithClaims(jwt.SigningMethodRS512, claims)
		key, err := jwt.ParseRSAPrivateKeyFromPEM(utiltest.PrivateKey)
		require.NoError(t, err)
		tokenString, err := token.SignedString(key)
		require.NoError(t, err)

		_, _, err = mgr.VerifyToken(tokenString)
		require.Error(t, err)
		assert.ErrorIs(t, err, common.TokenVerificationErr)
	})

	t.Run("OIDC provider is external, TLS is configured", func(t *testing.T) {
		dexConfig := map[string]string{
			"url": "",
			"oidc.config": fmt.Sprintf(`
name: Test
issuer: %s
clientID: xxx
clientSecret: yyy
requestedScopes: ["oidc"]`, oidcTestServer.URL),
		}

		// This is not actually used in the test. The test only calls the OIDC test server. But a valid cert/key pair
		// must be set to test VerifyToken's behavior when Argo CD is configured with TLS enabled.
		secretConfig := map[string][]byte{
			"tls.crt": utiltest.Cert,
			"tls.key": utiltest.PrivateKey,
		}

		settingsMgr := settings.NewSettingsManager(context.Background(), getKubeClientWithConfig(dexConfig, secretConfig), "argocd")
		mgr := NewSessionManager(settingsMgr, "", nil, NewUserStateStorage(nil))
		mgr.verificationDelayNoiseEnabled = false

		claims := jwt.RegisteredClaims{Audience: jwt.ClaimStrings{"test-client"}, Subject: "admin", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour * 24))}
		claims.Issuer = oidcTestServer.URL
		token := jwt.NewWithClaims(jwt.SigningMethodRS512, claims)
		key, err := jwt.ParseRSAPrivateKeyFromPEM(utiltest.PrivateKey)
		require.NoError(t, err)
		tokenString, err := token.SignedString(key)
		require.NoError(t, err)

		_, _, err = mgr.VerifyToken(tokenString)
		require.Error(t, err)
		assert.ErrorIs(t, err, common.TokenVerificationErr)
	})

	t.Run("OIDC provider is Dex, TLS is configured", func(t *testing.T) {
		dexConfig := map[string]string{
			"url": dexTestServer.URL,
			"dex.config": `connectors:
- type: github
  name: GitHub
  config:
    clientID: aabbccddeeff00112233
    clientSecret: aabbccddeeff00112233`,
		}

		// This is not actually used in the test. The test only calls the OIDC test server. But a valid cert/key pair
		// must be set to test VerifyToken's behavior when Argo CD is configured with TLS enabled.
		secretConfig := map[string][]byte{
			"tls.crt": utiltest.Cert,
			"tls.key": utiltest.PrivateKey,
		}

		settingsMgr := settings.NewSettingsManager(context.Background(), getKubeClientWithConfig(dexConfig, secretConfig), "argocd")
		mgr := NewSessionManager(settingsMgr, dexTestServer.URL, nil, NewUserStateStorage(nil))
		mgr.verificationDelayNoiseEnabled = false

		claims := jwt.RegisteredClaims{Audience: jwt.ClaimStrings{"test-client"}, Subject: "admin", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour * 24))}
		claims.Issuer = fmt.Sprintf("%s/api/dex", dexTestServer.URL)
		token := jwt.NewWithClaims(jwt.SigningMethodRS512, claims)
		key, err := jwt.ParseRSAPrivateKeyFromPEM(utiltest.PrivateKey)
		require.NoError(t, err)
		tokenString, err := token.SignedString(key)
		require.NoError(t, err)

		_, _, err = mgr.VerifyToken(tokenString)
		require.Error(t, err)
		assert.ErrorIs(t, err, common.TokenVerificationErr)
	})

	t.Run("OIDC provider is external, TLS is configured, OIDCTLSInsecureSkipVerify is true", func(t *testing.T) {
		dexConfig := map[string]string{
			"url": "",
			"oidc.config": fmt.Sprintf(`
name: Test
issuer: %s
clientID: xxx
clientSecret: yyy
requestedScopes: ["oidc"]`, oidcTestServer.URL),
			"oidc.tls.insecure.skip.verify": "true",
		}

		// This is not actually used in the test. The test only calls the OIDC test server. But a valid cert/key pair
		// must be set to test VerifyToken's behavior when Argo CD is configured with TLS enabled.
		secretConfig := map[string][]byte{
			"tls.crt": utiltest.Cert,
			"tls.key": utiltest.PrivateKey,
		}

		settingsMgr := settings.NewSettingsManager(context.Background(), getKubeClientWithConfig(dexConfig, secretConfig), "argocd")
		mgr := NewSessionManager(settingsMgr, "", nil, NewUserStateStorage(nil))
		mgr.verificationDelayNoiseEnabled = false

		claims := jwt.RegisteredClaims{Audience: jwt.ClaimStrings{"test-client"}, Subject: "admin", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour * 24))}
		claims.Issuer = oidcTestServer.URL
		token := jwt.NewWithClaims(jwt.SigningMethodRS512, claims)
		key, err := jwt.ParseRSAPrivateKeyFromPEM(utiltest.PrivateKey)
		require.NoError(t, err)
		tokenString, err := token.SignedString(key)
		require.NoError(t, err)

		_, _, err = mgr.VerifyToken(tokenString)
		assert.NotContains(t, err.Error(), "certificate is not trusted")
		assert.NotContains(t, err.Error(), "certificate signed by unknown authority")
	})

	t.Run("OIDC provider is external, TLS is not configured, OIDCTLSInsecureSkipVerify is true", func(t *testing.T) {
		dexConfig := map[string]string{
			"url": "",
			"oidc.config": fmt.Sprintf(`
name: Test
issuer: %s
clientID: xxx
clientSecret: yyy
requestedScopes: ["oidc"]`, oidcTestServer.URL),
			"oidc.tls.insecure.skip.verify": "true",
		}

		settingsMgr := settings.NewSettingsManager(context.Background(), getKubeClientWithConfig(dexConfig, nil), "argocd")
		mgr := NewSessionManager(settingsMgr, "", nil, NewUserStateStorage(nil))
		mgr.verificationDelayNoiseEnabled = false

		claims := jwt.RegisteredClaims{Audience: jwt.ClaimStrings{"test-client"}, Subject: "admin", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour * 24))}
		claims.Issuer = oidcTestServer.URL
		token := jwt.NewWithClaims(jwt.SigningMethodRS512, claims)
		key, err := jwt.ParseRSAPrivateKeyFromPEM(utiltest.PrivateKey)
		require.NoError(t, err)
		tokenString, err := token.SignedString(key)
		require.NoError(t, err)

		_, _, err = mgr.VerifyToken(tokenString)
		// This is the error thrown when the test server's certificate _is_ being verified.
		assert.NotContains(t, err.Error(), "certificate is not trusted")
		assert.NotContains(t, err.Error(), "certificate signed by unknown authority")
	})

	t.Run("OIDC provider is external, audience is not specified", func(t *testing.T) {
		config := map[string]string{
			"url": "",
			"oidc.config": fmt.Sprintf(`
name: Test
issuer: %s
clientID: xxx
clientSecret: yyy
requestedScopes: ["oidc"]`, oidcTestServer.URL),
			"oidc.tls.insecure.skip.verify": "true", // This isn't what we're testing.
		}

		// This is not actually used in the test. The test only calls the OIDC test server. But a valid cert/key pair
		// must be set to test VerifyToken's behavior when Argo CD is configured with TLS enabled.
		secretConfig := map[string][]byte{
			"tls.crt": utiltest.Cert,
			"tls.key": utiltest.PrivateKey,
		}

		settingsMgr := settings.NewSettingsManager(context.Background(), getKubeClientWithConfig(config, secretConfig), "argocd")
		mgr := NewSessionManager(settingsMgr, "", nil, NewUserStateStorage(nil))
		mgr.verificationDelayNoiseEnabled = false

		claims := jwt.RegisteredClaims{Subject: "admin", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour * 24))}
		claims.Issuer = oidcTestServer.URL
		token := jwt.NewWithClaims(jwt.SigningMethodRS512, claims)
		key, err := jwt.ParseRSAPrivateKeyFromPEM(utiltest.PrivateKey)
		require.NoError(t, err)
		tokenString, err := token.SignedString(key)
		require.NoError(t, err)

		_, _, err = mgr.VerifyToken(tokenString)
		require.Error(t, err)
	})

	t.Run("OIDC provider is external, audience is not specified, absent audience is allowed", func(t *testing.T) {
		config := map[string]string{
			"url": "",
			"oidc.config": fmt.Sprintf(`
name: Test
issuer: %s
clientID: xxx
clientSecret: yyy
requestedScopes: ["oidc"]
skipAudienceCheckWhenTokenHasNoAudience: true`, oidcTestServer.URL),
			"oidc.tls.insecure.skip.verify": "true", // This isn't what we're testing.
		}

		// This is not actually used in the test. The test only calls the OIDC test server. But a valid cert/key pair
		// must be set to test VerifyToken's behavior when Argo CD is configured with TLS enabled.
		secretConfig := map[string][]byte{
			"tls.crt": utiltest.Cert,
			"tls.key": utiltest.PrivateKey,
		}

		settingsMgr := settings.NewSettingsManager(context.Background(), getKubeClientWithConfig(config, secretConfig), "argocd")
		mgr := NewSessionManager(settingsMgr, "", nil, NewUserStateStorage(nil))
		mgr.verificationDelayNoiseEnabled = false

		claims := jwt.RegisteredClaims{Subject: "admin", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour * 24))}
		claims.Issuer = oidcTestServer.URL
		token := jwt.NewWithClaims(jwt.SigningMethodRS512, claims)
		key, err := jwt.ParseRSAPrivateKeyFromPEM(utiltest.PrivateKey)
		require.NoError(t, err)
		tokenString, err := token.SignedString(key)
		require.NoError(t, err)

		_, _, err = mgr.VerifyToken(tokenString)
		require.NoError(t, err)
	})

	t.Run("OIDC provider is external, audience is not specified but is required", func(t *testing.T) {
		config := map[string]string{
			"url": "",
			"oidc.config": fmt.Sprintf(`
name: Test
issuer: %s
clientID: xxx
clientSecret: yyy
requestedScopes: ["oidc"]
skipAudienceCheckWhenTokenHasNoAudience: false`, oidcTestServer.URL),
			"oidc.tls.insecure.skip.verify": "true", // This isn't what we're testing.
		}

		// This is not actually used in the test. The test only calls the OIDC test server. But a valid cert/key pair
		// must be set to test VerifyToken's behavior when Argo CD is configured with TLS enabled.
		secretConfig := map[string][]byte{
			"tls.crt": utiltest.Cert,
			"tls.key": utiltest.PrivateKey,
		}

		settingsMgr := settings.NewSettingsManager(context.Background(), getKubeClientWithConfig(config, secretConfig), "argocd")
		mgr := NewSessionManager(settingsMgr, "", nil, NewUserStateStorage(nil))
		mgr.verificationDelayNoiseEnabled = false

		claims := jwt.RegisteredClaims{Subject: "admin", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour * 24))}
		claims.Issuer = oidcTestServer.URL
		token := jwt.NewWithClaims(jwt.SigningMethodRS512, claims)
		key, err := jwt.ParseRSAPrivateKeyFromPEM(utiltest.PrivateKey)
		require.NoError(t, err)
		tokenString, err := token.SignedString(key)
		require.NoError(t, err)

		_, _, err = mgr.VerifyToken(tokenString)
		require.Error(t, err)
		assert.ErrorIs(t, err, common.TokenVerificationErr)
	})

	t.Run("OIDC provider is external, audience is client ID, no allowed list specified", func(t *testing.T) {
		config := map[string]string{
			"url": "",
			"oidc.config": fmt.Sprintf(`
name: Test
issuer: %s
clientID: xxx
clientSecret: yyy
requestedScopes: ["oidc"]`, oidcTestServer.URL),
			"oidc.tls.insecure.skip.verify": "true", // This isn't what we're testing.
		}

		// This is not actually used in the test. The test only calls the OIDC test server. But a valid cert/key pair
		// must be set to test VerifyToken's behavior when Argo CD is configured with TLS enabled.
		secretConfig := map[string][]byte{
			"tls.crt": utiltest.Cert,
			"tls.key": utiltest.PrivateKey,
		}

		settingsMgr := settings.NewSettingsManager(context.Background(), getKubeClientWithConfig(config, secretConfig), "argocd")
		mgr := NewSessionManager(settingsMgr, "", nil, NewUserStateStorage(nil))
		mgr.verificationDelayNoiseEnabled = false

		claims := jwt.RegisteredClaims{Audience: jwt.ClaimStrings{"xxx"}, Subject: "admin", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour * 24))}
		claims.Issuer = oidcTestServer.URL
		token := jwt.NewWithClaims(jwt.SigningMethodRS512, claims)
		key, err := jwt.ParseRSAPrivateKeyFromPEM(utiltest.PrivateKey)
		require.NoError(t, err)
		tokenString, err := token.SignedString(key)
		require.NoError(t, err)

		_, _, err = mgr.VerifyToken(tokenString)
		require.NoError(t, err)
	})

	t.Run("OIDC provider is external, audience is in allowed list", func(t *testing.T) {
		config := map[string]string{
			"url": "",
			"oidc.config": fmt.Sprintf(`
name: Test
issuer: %s
clientID: xxx
clientSecret: yyy
requestedScopes: ["oidc"]
allowedAudiences:
- something`, oidcTestServer.URL),
			"oidc.tls.insecure.skip.verify": "true", // This isn't what we're testing.
		}

		// This is not actually used in the test. The test only calls the OIDC test server. But a valid cert/key pair
		// must be set to test VerifyToken's behavior when Argo CD is configured with TLS enabled.
		secretConfig := map[string][]byte{
			"tls.crt": utiltest.Cert,
			"tls.key": utiltest.PrivateKey,
		}

		settingsMgr := settings.NewSettingsManager(context.Background(), getKubeClientWithConfig(config, secretConfig), "argocd")
		mgr := NewSessionManager(settingsMgr, "", nil, NewUserStateStorage(nil))
		mgr.verificationDelayNoiseEnabled = false

		claims := jwt.RegisteredClaims{Audience: jwt.ClaimStrings{"something"}, Subject: "admin", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour * 24))}
		claims.Issuer = oidcTestServer.URL
		token := jwt.NewWithClaims(jwt.SigningMethodRS512, claims)
		key, err := jwt.ParseRSAPrivateKeyFromPEM(utiltest.PrivateKey)
		require.NoError(t, err)
		tokenString, err := token.SignedString(key)
		require.NoError(t, err)

		_, _, err = mgr.VerifyToken(tokenString)
		require.NoError(t, err)
	})

	t.Run("OIDC provider is external, audience is not in allowed list", func(t *testing.T) {
		config := map[string]string{
			"url": "",
			"oidc.config": fmt.Sprintf(`
name: Test
issuer: %s
clientID: xxx
clientSecret: yyy
requestedScopes: ["oidc"]
allowedAudiences:
- something-else`, oidcTestServer.URL),
			"oidc.tls.insecure.skip.verify": "true", // This isn't what we're testing.
		}

		// This is not actually used in the test. The test only calls the OIDC test server. But a valid cert/key pair
		// must be set to test VerifyToken's behavior when Argo CD is configured with TLS enabled.
		secretConfig := map[string][]byte{
			"tls.crt": utiltest.Cert,
			"tls.key": utiltest.PrivateKey,
		}

		settingsMgr := settings.NewSettingsManager(context.Background(), getKubeClientWithConfig(config, secretConfig), "argocd")
		mgr := NewSessionManager(settingsMgr, "", nil, NewUserStateStorage(nil))
		mgr.verificationDelayNoiseEnabled = false

		claims := jwt.RegisteredClaims{Audience: jwt.ClaimStrings{"something"}, Subject: "admin", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour * 24))}
		claims.Issuer = oidcTestServer.URL
		token := jwt.NewWithClaims(jwt.SigningMethodRS512, claims)
		key, err := jwt.ParseRSAPrivateKeyFromPEM(utiltest.PrivateKey)
		require.NoError(t, err)
		tokenString, err := token.SignedString(key)
		require.NoError(t, err)

		_, _, err = mgr.VerifyToken(tokenString)
		require.Error(t, err)
		assert.ErrorIs(t, err, common.TokenVerificationErr)
	})

	t.Run("OIDC provider is external, audience is not client ID, and there is no allow list", func(t *testing.T) {
		config := map[string]string{
			"url": "",
			"oidc.config": fmt.Sprintf(`
name: Test
issuer: %s
clientID: xxx
clientSecret: yyy
requestedScopes: ["oidc"]`, oidcTestServer.URL),
			"oidc.tls.insecure.skip.verify": "true", // This isn't what we're testing.
		}

		// This is not actually used in the test. The test only calls the OIDC test server. But a valid cert/key pair
		// must be set to test VerifyToken's behavior when Argo CD is configured with TLS enabled.
		secretConfig := map[string][]byte{
			"tls.crt": utiltest.Cert,
			"tls.key": utiltest.PrivateKey,
		}

		settingsMgr := settings.NewSettingsManager(context.Background(), getKubeClientWithConfig(config, secretConfig), "argocd")
		mgr := NewSessionManager(settingsMgr, "", nil, NewUserStateStorage(nil))
		mgr.verificationDelayNoiseEnabled = false

		claims := jwt.RegisteredClaims{Audience: jwt.ClaimStrings{"something"}, Subject: "admin", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour * 24))}
		claims.Issuer = oidcTestServer.URL
		token := jwt.NewWithClaims(jwt.SigningMethodRS512, claims)
		key, err := jwt.ParseRSAPrivateKeyFromPEM(utiltest.PrivateKey)
		require.NoError(t, err)
		tokenString, err := token.SignedString(key)
		require.NoError(t, err)

		_, _, err = mgr.VerifyToken(tokenString)
		require.Error(t, err)
		assert.ErrorIs(t, err, common.TokenVerificationErr)
	})

	t.Run("OIDC provider is external, audience is specified, but allow list is empty", func(t *testing.T) {
		config := map[string]string{
			"url": "",
			"oidc.config": fmt.Sprintf(`
name: Test
issuer: %s
clientID: xxx
clientSecret: yyy
requestedScopes: ["oidc"]
allowedAudiences: []`, oidcTestServer.URL),
			"oidc.tls.insecure.skip.verify": "true", // This isn't what we're testing.
		}

		// This is not actually used in the test. The test only calls the OIDC test server. But a valid cert/key pair
		// must be set to test VerifyToken's behavior when Argo CD is configured with TLS enabled.
		secretConfig := map[string][]byte{
			"tls.crt": utiltest.Cert,
			"tls.key": utiltest.PrivateKey,
		}

		settingsMgr := settings.NewSettingsManager(context.Background(), getKubeClientWithConfig(config, secretConfig), "argocd")
		mgr := NewSessionManager(settingsMgr, "", nil, NewUserStateStorage(nil))
		mgr.verificationDelayNoiseEnabled = false

		claims := jwt.RegisteredClaims{Audience: jwt.ClaimStrings{"something"}, Subject: "admin", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour * 24))}
		claims.Issuer = oidcTestServer.URL
		token := jwt.NewWithClaims(jwt.SigningMethodRS512, claims)
		key, err := jwt.ParseRSAPrivateKeyFromPEM(utiltest.PrivateKey)
		require.NoError(t, err)
		tokenString, err := token.SignedString(key)
		require.NoError(t, err)

		_, _, err = mgr.VerifyToken(tokenString)
		require.Error(t, err)
		assert.ErrorIs(t, err, common.TokenVerificationErr)
	})

	// Make sure the logic works to allow any of the allowed audiences, not just the first one.
	t.Run("OIDC provider is external, audience is specified, actual audience isn't the first allowed audience", func(t *testing.T) {
		config := map[string]string{
			"url": "",
			"oidc.config": fmt.Sprintf(`
name: Test
issuer: %s
clientID: xxx
clientSecret: yyy
requestedScopes: ["oidc"]
allowedAudiences: ["aud-a", "aud-b"]`, oidcTestServer.URL),
			"oidc.tls.insecure.skip.verify": "true", // This isn't what we're testing.
		}

		// This is not actually used in the test. The test only calls the OIDC test server. But a valid cert/key pair
		// must be set to test VerifyToken's behavior when Argo CD is configured with TLS enabled.
		secretConfig := map[string][]byte{
			"tls.crt": utiltest.Cert,
			"tls.key": utiltest.PrivateKey,
		}

		settingsMgr := settings.NewSettingsManager(context.Background(), getKubeClientWithConfig(config, secretConfig), "argocd")
		mgr := NewSessionManager(settingsMgr, "", nil, NewUserStateStorage(nil))
		mgr.verificationDelayNoiseEnabled = false

		claims := jwt.RegisteredClaims{Audience: jwt.ClaimStrings{"aud-b"}, Subject: "admin", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour * 24))}
		claims.Issuer = oidcTestServer.URL
		token := jwt.NewWithClaims(jwt.SigningMethodRS512, claims)
		key, err := jwt.ParseRSAPrivateKeyFromPEM(utiltest.PrivateKey)
		require.NoError(t, err)
		tokenString, err := token.SignedString(key)
		require.NoError(t, err)

		_, _, err = mgr.VerifyToken(tokenString)
		require.NoError(t, err)
	})

	t.Run("OIDC provider is external, audience is not specified, token is signed with the wrong key", func(t *testing.T) {
		config := map[string]string{
			"url": "",
			"oidc.config": fmt.Sprintf(`
name: Test
issuer: %s
clientID: xxx
clientSecret: yyy
requestedScopes: ["oidc"]`, oidcTestServer.URL),
			"oidc.tls.insecure.skip.verify": "true", // This isn't what we're testing.
		}

		// This is not actually used in the test. The test only calls the OIDC test server. But a valid cert/key pair
		// must be set to test VerifyToken's behavior when Argo CD is configured with TLS enabled.
		secretConfig := map[string][]byte{
			"tls.crt": utiltest.Cert,
			"tls.key": utiltest.PrivateKey,
		}

		settingsMgr := settings.NewSettingsManager(context.Background(), getKubeClientWithConfig(config, secretConfig), "argocd")
		mgr := NewSessionManager(settingsMgr, "", nil, NewUserStateStorage(nil))
		mgr.verificationDelayNoiseEnabled = false

		claims := jwt.RegisteredClaims{Subject: "admin", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour * 24))}
		claims.Issuer = oidcTestServer.URL
		token := jwt.NewWithClaims(jwt.SigningMethodRS512, claims)
		key, err := jwt.ParseRSAPrivateKeyFromPEM(utiltest.PrivateKey2)
		require.NoError(t, err)
		tokenString, err := token.SignedString(key)
		require.NoError(t, err)

		_, _, err = mgr.VerifyToken(tokenString)
		require.Error(t, err)
		assert.ErrorIs(t, err, common.TokenVerificationErr)
	})
}
