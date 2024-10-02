package settings

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/argoproj/argo-rollouts/common"
	testutil "github.com/argoproj/argo-rollouts/test"
	"github.com/argoproj/argo-rollouts/utils/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func fixtures(data map[string]string, opts ...func(secret *v1.Secret)) (*fake.Clientset, *SettingsManager) {
	cm := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      common.ArgoRolloutsConfigMapName,
			Namespace: "default",
			Labels: map[string]string{
				"app.kubernetes.io/part-of": "argo-rollouts",
			},
		},
		Data: data,
	}
	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      common.ArgoRolloutsSecretName,
			Namespace: "default",
			Labels: map[string]string{
				"app.kubernetes.io/part-of": "argo-rollouts",
			},
		},
		Data: map[string][]byte{},
	}
	for i := range opts {
		opts[i](secret)
	}
	kubeClient := fake.NewSimpleClientset(cm, secret)
	settingsManager := NewSettingsManager(context.Background(), kubeClient, "default")

	return kubeClient, settingsManager
}

func TestInClusterServerAddressEnabled(t *testing.T) {
	_, settingsManager := fixtures(map[string]string{
		"cluster.inClusterEnabled": "true",
	})
	argoRolloutsCM, err := settingsManager.getConfigMap()
	require.NoError(t, err)
	assert.Equal(t, "true", argoRolloutsCM.Data[inClusterEnabledKey])

	_, settingsManager = fixtures(map[string]string{
		"cluster.inClusterEnabled": "false",
	})
	argoRolloutsCM, err = settingsManager.getConfigMap()
	require.NoError(t, err)
	assert.NotEqual(t, "true", argoRolloutsCM.Data[inClusterEnabledKey])
}

func TestInClusterServerAddressEnabledByDefault(t *testing.T) {
	kubeClient := fake.NewSimpleClientset(
		&v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      common.ArgoRolloutsConfigMapName,
				Namespace: "default",
				Labels: map[string]string{
					"app.kubernetes.io/part-of": "argo-rollouts",
				},
			},
			Data: map[string]string{},
		},
		&v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      common.ArgoRolloutsSecretName,
				Namespace: "default",
				Labels: map[string]string{
					"app.kubernetes.io/part-of": "argo-rollouts",
				},
			},
			Data: map[string][]byte{
				"admin.password":   nil,
				"server.secretkey": nil,
			},
		},
	)
	settingsManager := NewSettingsManager(context.Background(), kubeClient, "default")
	settings, err := settingsManager.GetSettings()
	require.NoError(t, err)
	assert.True(t, settings.InClusterEnabled)
}

func TestConvertToOverrideKey(t *testing.T) {
	key, err := convertToOverrideKey("cert-manager.io_Certificate")
	require.NoError(t, err)
	assert.Equal(t, "cert-manager.io/Certificate", key)

	key, err = convertToOverrideKey("Certificate")
	require.NoError(t, err)
	assert.Equal(t, "Certificate", key)

	_, err = convertToOverrideKey("")
	require.Error(t, err)

	_, err = convertToOverrideKey("_")
	require.NoError(t, err)
}

func TestSettingsManager_GetSettings(t *testing.T) {
	t.Run("UserSessionDurationNotProvided", func(t *testing.T) {
		kubeClient := fake.NewSimpleClientset(
			&v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      common.ArgoRolloutsConfigMapName,
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/part-of": "argo-rollouts",
					},
				},
				Data: nil,
			},
			&v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      common.ArgoRolloutsSecretName,
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/part-of": "argo-rollouts",
					},
				},
				Data: map[string][]byte{
					"server.secretkey": nil,
				},
			},
		)
		settingsManager := NewSettingsManager(context.Background(), kubeClient, "default")
		s, err := settingsManager.GetSettings()
		require.NoError(t, err)
		assert.Equal(t, time.Hour*24, s.UserSessionDuration)
	})
	t.Run("UserSessionDurationInvalidFormat", func(t *testing.T) {
		kubeClient := fake.NewSimpleClientset(
			&v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      common.ArgoRolloutsConfigMapName,
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/part-of": "argo-rollouts",
					},
				},
				Data: map[string]string{
					"users.session.duration": "10hh",
				},
			},
			&v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      common.ArgoRolloutsSecretName,
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/part-of": "argo-rollouts",
					},
				},
				Data: map[string][]byte{
					"server.secretkey": nil,
				},
			},
		)
		settingsManager := NewSettingsManager(context.Background(), kubeClient, "default")
		s, err := settingsManager.GetSettings()
		require.NoError(t, err)
		assert.Equal(t, time.Hour*24, s.UserSessionDuration)
	})
	t.Run("UserSessionDurationProvided", func(t *testing.T) {
		kubeClient := fake.NewSimpleClientset(
			&v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      common.ArgoRolloutsConfigMapName,
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/part-of": "argo-rollouts",
					},
				},
				Data: map[string]string{
					"users.session.duration": "10h",
				},
			},
			&v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      common.ArgoRolloutsSecretName,
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/part-of": "argo-rollouts",
					},
				},
				Data: map[string][]byte{
					"server.secretkey": nil,
				},
			},
		)
		settingsManager := NewSettingsManager(context.Background(), kubeClient, "default")
		s, err := settingsManager.GetSettings()
		require.NoError(t, err)
		assert.Equal(t, time.Hour*10, s.UserSessionDuration)
	})
}

func TestGetOIDCConfig(t *testing.T) {
	kubeClient := fake.NewSimpleClientset(
		&v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      common.ArgoRolloutsConfigMapName,
				Namespace: "default",
				Labels: map[string]string{
					"app.kubernetes.io/part-of": "argo-rollouts",
				},
			},
			Data: map[string]string{
				"oidc.config": "\n  requestedIDTokenClaims: {\"groups\": {\"essential\": true}}\n",
			},
		},
		&v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      common.ArgoRolloutsSecretName,
				Namespace: "default",
				Labels: map[string]string{
					"app.kubernetes.io/part-of": "argo-rollouts",
				},
			},
			Data: map[string][]byte{
				"admin.password":   nil,
				"server.secretkey": nil,
			},
		},
	)
	settingsManager := NewSettingsManager(context.Background(), kubeClient, "default")
	settings, err := settingsManager.GetSettings()
	require.NoError(t, err)

	oidcConfig := settings.OIDCConfig()
	assert.NotNil(t, oidcConfig)

	claim := oidcConfig.RequestedIDTokenClaims["groups"]
	assert.NotNil(t, claim)
	assert.True(t, claim.Essential)
}

func TestRedirectURL(t *testing.T) {
	cases := map[string][]string{
		"https://localhost:4000":               {"https://localhost:4000/auth/callback", "https://localhost:4000/api/dex/callback"},
		"https://localhost:4000/":              {"https://localhost:4000/auth/callback", "https://localhost:4000/api/dex/callback"},
		"https://localhost:4000/argorollouts":  {"https://localhost:4000/argorollouts/auth/callback", "https://localhost:4000/argorollouts/api/dex/callback"},
		"https://localhost:4000/argorollouts/": {"https://localhost:4000/argorollouts/auth/callback", "https://localhost:4000/argorollouts/api/dex/callback"},
	}
	for given, expected := range cases {
		settings := ArgoRolloutsSettings{URL: given}
		redirectURL, err := settings.RedirectURL()
		require.NoError(t, err)
		assert.Equal(t, expected[0], redirectURL)
		dexRedirectURL, err := settings.DexRedirectURL()
		require.NoError(t, err)
		assert.Equal(t, expected[1], dexRedirectURL)
	}
}

func Test_validateExternalURL(t *testing.T) {
	tests := []struct {
		name   string
		url    string
		errMsg string
	}{
		{name: "Valid URL", url: "https://my.domain.com"},
		{name: "No URL - Valid", url: ""},
		{name: "Invalid URL", url: "my.domain.com", errMsg: "URL must include http or https protocol"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateExternalURL(tt.url)
			if tt.errMsg != "" {
				assert.EqualError(t, err, tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestGetOIDCSecretTrim(t *testing.T) {
	kubeClient := fake.NewSimpleClientset(
		&v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      common.ArgoRolloutsConfigMapName,
				Namespace: "default",
				Labels: map[string]string{
					"app.kubernetes.io/part-of": "argo-rollouts",
				},
			},
			Data: map[string]string{
				"oidc.config": "\n  name: Okta\n  clientSecret: test-secret\r\n \n  clientID: aaaabbbbccccddddeee\n",
			},
		},
		&v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      common.ArgoRolloutsSecretName,
				Namespace: "default",
				Labels: map[string]string{
					"app.kubernetes.io/part-of": "argo-rollouts",
				},
			},
			Data: map[string][]byte{
				"admin.password":   nil,
				"server.secretkey": nil,
			},
		},
	)
	settingsManager := NewSettingsManager(context.Background(), kubeClient, "default")
	settings, err := settingsManager.GetSettings()
	require.NoError(t, err)

	oidcConfig := settings.OIDCConfig()
	assert.NotNil(t, oidcConfig)
	assert.Equal(t, "test-secret", oidcConfig.ClientSecret)
}

func getCNFromCertificate(cert *tls.Certificate) string {
	c, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return ""
	}
	return c.Subject.CommonName
}

func Test_GetTLSConfiguration(t *testing.T) {
	t.Run("Valid external TLS secret with success", func(t *testing.T) {
		kubeClient := fake.NewSimpleClientset(
			&v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      common.ArgoRolloutsConfigMapName,
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/part-of": "argo-rollouts",
					},
				},
				Data: map[string]string{
					"oidc.config": "\n  name: Okta\n  clientSecret: test-secret\r\n \n  clientID: aaaabbbbccccddddeee\n",
				},
			},
			&v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      common.ArgoRolloutsSecretName,
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/part-of": "argo-rollouts",
					},
				},
				Data: map[string][]byte{
					"admin.password":   nil,
					"server.secretkey": nil,
				},
			},
			&v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      externalServerTLSSecretName,
					Namespace: "default",
				},
				Data: map[string][]byte{
					"tls.crt": []byte(testutil.MustLoadFileToString("../../test/fixtures/certs/argocd-test-server.crt")),
					"tls.key": []byte(testutil.MustLoadFileToString("../../test/fixtures/certs/argocd-test-server.key")),
				},
			},
		)
		settingsManager := NewSettingsManager(context.Background(), kubeClient, "default")
		settings, err := settingsManager.GetSettings()
		require.NoError(t, err)
		assert.True(t, settings.CertificateIsExternal)
		assert.NotNil(t, settings.Certificate)
		assert.Contains(t, getCNFromCertificate(settings.Certificate), "localhost")
	})

	t.Run("Valid external TLS secret overrides argocd-secret", func(t *testing.T) {
		kubeClient := fake.NewSimpleClientset(
			&v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      common.ArgoRolloutsConfigMapName,
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/part-of": "argo-rollouts",
					},
				},
				Data: map[string]string{
					"oidc.config": "\n  name: Okta\n  clientSecret: test-secret\r\n \n  clientID: aaaabbbbccccddddeee\n",
				},
			},
			&v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      common.ArgoRolloutsSecretName,
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/part-of": "argo-rollouts",
					},
				},
				Data: map[string][]byte{
					"admin.password":   nil,
					"server.secretkey": nil,
					"tls.crt":          []byte(testutil.MustLoadFileToString("../../test/fixtures/certs/argocd-e2e-server.crt")),
					"tls.key":          []byte(testutil.MustLoadFileToString("../../test/fixtures/certs/argocd-e2e-server.key")),
				},
			},
			&v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      externalServerTLSSecretName,
					Namespace: "default",
				},
				Data: map[string][]byte{
					"tls.crt": []byte(testutil.MustLoadFileToString("../../test/fixtures/certs/argocd-test-server.crt")),
					"tls.key": []byte(testutil.MustLoadFileToString("../../test/fixtures/certs/argocd-test-server.key")),
				},
			},
		)
		settingsManager := NewSettingsManager(context.Background(), kubeClient, "default")
		settings, err := settingsManager.GetSettings()
		require.NoError(t, err)
		assert.True(t, settings.CertificateIsExternal)
		assert.NotNil(t, settings.Certificate)
		assert.Contains(t, getCNFromCertificate(settings.Certificate), "localhost")
	})
	t.Run("Invalid external TLS secret", func(t *testing.T) {
		kubeClient := fake.NewSimpleClientset(
			&v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      common.ArgoRolloutsConfigMapName,
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/part-of": "argo-rollouts",
					},
				},
				Data: map[string]string{
					"oidc.config": "\n  name: Okta\n  clientSecret: test-secret\r\n \n  clientID: aaaabbbbccccddddeee\n",
				},
			},
			&v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      common.ArgoRolloutsSecretName,
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/part-of": "argo-rollouts",
					},
				},
				Data: map[string][]byte{
					"admin.password":   nil,
					"server.secretkey": nil,
				},
			},
			&v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      externalServerTLSSecretName,
					Namespace: "default",
				},
				Data: map[string][]byte{
					"tls.crt": []byte(""),
					"tls.key": []byte(""),
				},
			},
		)
		settingsManager := NewSettingsManager(context.Background(), kubeClient, "default")
		settings, err := settingsManager.GetSettings()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "could not read from secret")
		assert.NotNil(t, settings)
	})
	t.Run("No external TLS secret", func(t *testing.T) {
		kubeClient := fake.NewSimpleClientset(
			&v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      common.ArgoRolloutsConfigMapName,
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/part-of": "argo-rollouts",
					},
				},
				Data: map[string]string{
					"oidc.config": "\n  name: Okta\n  clientSecret: test-secret\r\n \n  clientID: aaaabbbbccccddddeee\n",
				},
			},
			&v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      common.ArgoRolloutsSecretName,
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/part-of": "argo-rollouts",
					},
				},
				Data: map[string][]byte{
					"admin.password":   nil,
					"server.secretkey": nil,
					"tls.crt":          []byte(testutil.MustLoadFileToString("../../test/fixtures/certs/argocd-e2e-server.crt")),
					"tls.key":          []byte(testutil.MustLoadFileToString("../../test/fixtures/certs/argocd-e2e-server.key")),
				},
			},
		)
		settingsManager := NewSettingsManager(context.Background(), kubeClient, "default")
		settings, err := settingsManager.GetSettings()
		require.NoError(t, err)
		assert.False(t, settings.CertificateIsExternal)
		assert.NotNil(t, settings.Certificate)
		assert.Contains(t, getCNFromCertificate(settings.Certificate), "Argo CD E2E")
	})
}

func TestDownloadArgoCDBinaryUrls(t *testing.T) {
	_, settingsManager := fixtures(map[string]string{
		"help.download.darwin-amd64": "some-url",
	})
	argoRolloutsCM, err := settingsManager.getConfigMap()
	require.NoError(t, err)
	assert.Equal(t, "some-url", argoRolloutsCM.Data["help.download.darwin-amd64"])

	_, settingsManager = fixtures(map[string]string{
		"help.download.linux-s390x": "some-url",
	})
	argoRolloutsCM, err = settingsManager.getConfigMap()
	require.NoError(t, err)
	assert.Equal(t, "some-url", argoRolloutsCM.Data["help.download.linux-s390x"])

	_, settingsManager = fixtures(map[string]string{
		"help.download.unsupported": "some-url",
	})
	argoRolloutsCM, err = settingsManager.getConfigMap()
	require.NoError(t, err)
	assert.Equal(t, "some-url", argoRolloutsCM.Data["help.download.unsupported"])
}

func TestSecretKeyRef(t *testing.T) {
	data := map[string]string{
		"oidc.config": `name: Okta
issuer: $ext:issuerSecret
clientID: aaaabbbbccccddddeee
clientSecret: $ext:clientSecret
# Optional set of OIDC scopes to request. If omitted, defaults to: ["openid", "profile", "email", "groups"]
requestedScopes: ["openid", "profile", "email"]
# Optional set of OIDC claims to request on the ID token.
requestedIDTokenClaims: {"groups": {"essential": true}}`,
	}
	cm := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      common.ArgoRolloutsConfigMapName,
			Namespace: "default",
			Labels: map[string]string{
				"app.kubernetes.io/part-of": "argo-rollouts",
			},
		},
		Data: data,
	}
	argocdSecret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      common.ArgoRolloutsSecretName,
			Namespace: "default",
		},
		Data: map[string][]byte{
			"admin.password":   nil,
			"server.secretkey": nil,
		},
	}
	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ext",
			Namespace: "default",
			Labels: map[string]string{
				"app.kubernetes.io/part-of": "argo-rollouts",
			},
		},
		Data: map[string][]byte{
			"issuerSecret": []byte("https://dev-123456.oktapreview.com"),
			"clientSecret": []byte("deadbeef"),
		},
	}
	kubeClient := fake.NewSimpleClientset(cm, secret, argocdSecret)
	settingsManager := NewSettingsManager(context.Background(), kubeClient, "default")

	settings, err := settingsManager.GetSettings()
	require.NoError(t, err)

	oidcConfig := settings.OIDCConfig()
	assert.Equal(t, "https://dev-123456.oktapreview.com", oidcConfig.Issuer)
	assert.Equal(t, "deadbeef", oidcConfig.ClientSecret)
}

func TestArgoRolloutsSettings_OIDCTLSConfig_OIDCTLSInsecureSkipVerify(t *testing.T) {
	certParsed, err := tls.X509KeyPair(test.Cert, test.PrivateKey)
	require.NoError(t, err)

	testCases := []struct {
		name               string
		settings           *ArgoRolloutsSettings
		expectNilTLSConfig bool
	}{
		{
			name: "OIDC configured, no root CA",
			settings: &ArgoRolloutsSettings{OIDCConfigRAW: `name: Test
issuer: aaa
clientID: xxx
clientSecret: yyy
requestedScopes: ["oidc"]`},
		},
		{
			name: "OIDC configured, valid root CA",
			settings: &ArgoRolloutsSettings{OIDCConfigRAW: fmt.Sprintf(`
name: Test
issuer: aaa
clientID: xxx
clientSecret: yyy
requestedScopes: ["oidc"]
rootCA: |
  %s
`, strings.ReplaceAll(string(test.Cert), "\n", "\n  "))},
		},
		{
			name: "OIDC configured, invalid root CA",
			settings: &ArgoRolloutsSettings{OIDCConfigRAW: `name: Test
issuer: aaa
clientID: xxx
clientSecret: yyy
requestedScopes: ["oidc"]
rootCA: "invalid"`},
		},
		{
			name:               "OIDC not configured, no cert configured",
			settings:           &ArgoRolloutsSettings{},
			expectNilTLSConfig: true,
		},
		{
			name:     "OIDC not configured, cert configured",
			settings: &ArgoRolloutsSettings{Certificate: &certParsed},
		},
	}

	for _, testCase := range testCases {
		testCase := testCase

		t.Run(testCase.name, func(t *testing.T) {
			if testCase.expectNilTLSConfig {
				assert.Nil(t, testCase.settings.OIDCTLSConfig())
			} else {
				assert.False(t, testCase.settings.OIDCTLSConfig().InsecureSkipVerify)

				testCase.settings.OIDCTLSInsecureSkipVerify = true

				assert.True(t, testCase.settings.OIDCTLSConfig().InsecureSkipVerify)
			}
		})
	}
}

func Test_OAuth2AllowedAudiences(t *testing.T) {
	testCases := []struct {
		name     string
		settings *ArgoRolloutsSettings
		expected []string
	}{
		{
			name:     "Empty",
			settings: &ArgoRolloutsSettings{},
			expected: []string{},
		},
		{
			name: "OIDC configured, no audiences specified, clientID used",
			settings: &ArgoRolloutsSettings{OIDCConfigRAW: `name: Test
issuer: aaa
clientID: xxx
clientSecret: yyy
requestedScopes: ["oidc"]`},
			expected: []string{"xxx"},
		},
		{
			name: "OIDC configured, no audiences specified, clientID and cliClientID used",
			settings: &ArgoRolloutsSettings{OIDCConfigRAW: `name: Test
issuer: aaa
clientID: xxx
cliClientID: cli-xxx
clientSecret: yyy
requestedScopes: ["oidc"]`},
			expected: []string{"xxx", "cli-xxx"},
		},
		{
			name: "OIDC configured, audiences specified",
			settings: &ArgoRolloutsSettings{OIDCConfigRAW: `name: Test
issuer: aaa
clientID: xxx
clientSecret: yyy
requestedScopes: ["oidc"]
allowedAudiences: ["aud1", "aud2"]`},
			expected: []string{"aud1", "aud2"},
		},
		{
			name: "Dex configured",
			settings: &ArgoRolloutsSettings{DexConfig: `connectors:
  - type: github
    id: github
    name: GitHub
    config:
      clientID: aabbccddeeff00112233
      clientSecret: $dex.github.clientSecret
      orgs:
      - name: your-github-org
`},
			expected: []string{common.ArgoRolloutsClientAppID, common.ArgoRolloutsCLIClientAppID},
		},
	}

	for _, tc := range testCases {
		tcc := tc
		t.Run(tcc.name, func(t *testing.T) {
			t.Parallel()
			assert.ElementsMatch(t, tcc.expected, tcc.settings.OAuth2AllowedAudiences())
		})
	}
}

func TestReplaceStringSecret(t *testing.T) {
	secretValues := map[string]string{"my-secret-key": "my-secret-value"}
	result := ReplaceStringSecret("$my-secret-key", secretValues)
	assert.Equal(t, "my-secret-value", result)

	result = ReplaceStringSecret("$invalid-secret-key", secretValues)
	assert.Equal(t, "$invalid-secret-key", result)

	result = ReplaceStringSecret("", secretValues)
	assert.Equal(t, "", result)

	result = ReplaceStringSecret("my-value", secretValues)
	assert.Equal(t, "my-value", result)
}
