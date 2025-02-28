package datadog

import (
	"fmt"
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/tj/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	kubetesting "k8s.io/client-go/testing"
)

const (
	DatadogUrl = "datadog.com"
	ApiKey     = "apiKey"
	AppKey     = "appKey"
)

func TestRunSuit(t *testing.T) {

	testCases := []struct {
		name            string
		secret          *corev1.Secret
		expectedApiKey  string
		expectedAppKey  string
		expectedAddress string
		expectError     bool
	}{
		{
			name: "When secret valid, should be successful",
			secret: NewSecretBuilder().
				WithName("DatadogTokensSecretName").
				WithData("address", []byte(DatadogUrl)).
				WithData("api-key", []byte(ApiKey)).
				WithData("app-key", []byte(AppKey)).
				Build(),
			expectedApiKey:  ApiKey,
			expectedAppKey:  AppKey,
			expectedAddress: DatadogUrl,
		},
		{
			name: "When secret is found but no data, should return empty values",
			secret: NewSecretBuilder().
				WithName("DatadogTokensSecretName").
				Build(),
		},
		{
			name:        "When secret not found, should return empty values",
			expectError: true,
		},
	}

	for _, tc := range testCases {
		fakeClient := k8sfake.NewSimpleClientset()
		fakeClient.PrependReactor("get", "*", func(action kubetesting.Action) (handled bool, ret runtime.Object, err error) {
			if tc.expectError {
				return true, nil, fmt.Errorf("api error")
			}
			return true, tc.secret, nil
		})

		secretFinder := NewSecretFinder(fakeClient, "", "")
		t.Run(tc.name, func(t *testing.T) {
			logCtx := *log.WithField("test", "test")

			address, apiKey, appKey := secretFinder.FindCredentials(logCtx)
			assert.Equal(t, tc.expectedAddress, address)
			assert.Equal(t, tc.expectedApiKey, apiKey)
			assert.Equal(t, tc.expectedAppKey, appKey)
		})
	}

}

// SecretBuilder helps in constructing a corev1.Secret object
type SecretBuilder struct {
	name string
	data map[string][]byte
}

// NewSecretBuilder creates a new SecretBuilder
func NewSecretBuilder() *SecretBuilder {
	return &SecretBuilder{
		data: make(map[string][]byte),
	}
}

// WithName sets the name for the Secret
func (b *SecretBuilder) WithName(name string) *SecretBuilder {
	b.name = name
	return b
}

// WithData sets the data for the Secret
func (b *SecretBuilder) WithData(key string, value []byte) *SecretBuilder {
	b.data[key] = value
	return b
}

// Build constructs the corev1.Secret object
func (b *SecretBuilder) Build() *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: b.name,
		},
		Data: b.data,
	}
}

// NewSecretBuilderDefaultData creates a new SecretBuilder with default values
func NewSecretBuilderDefaultData() *SecretBuilder {
	return &SecretBuilder{
		data: map[string][]byte{
			"address": []byte("datadog.com"),
			"api-key": []byte("apiKey"),
			"app-key": []byte("appKey"),
		},
	}
}
