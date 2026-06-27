// Package settings loads the Argo Rollouts dashboard's authentication
// configuration (signing key, local accounts, RBAC) from Kubernetes
// ConfigMaps and a Secret.
package settings

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Kubernetes object names for the dashboard's auth configuration.
const (
	SecretName        = "argo-rollouts-dashboard-secret"
	ConfigMapName     = "argo-rollouts-dashboard-cm"
	RBACConfigMapName = "argo-rollouts-dashboard-rbac-cm"
)

// Secret data keys.
const (
	KeyServerSignature = "server.secretkey"
)

// MinSigningKeyLength is the minimum acceptable HS256 signing key length.
// A shorter key is trivially brute-forced, so it is rejected at load time.
const MinSigningKeyLength = 32

// SettingsManager reads dashboard auth settings from Kubernetes.
type SettingsManager struct {
	clientset kubernetes.Interface
	namespace string
}

// NewSettingsManager returns a SettingsManager that reads the dashboard
// Secret/ConfigMaps from namespace using clientset.
func NewSettingsManager(clientset kubernetes.Interface, namespace string) *SettingsManager {
	return &SettingsManager{clientset: clientset, namespace: namespace}
}

// secretData returns the dashboard Secret's data, or an empty map if the
// Secret does not exist. Only non-NotFound API errors propagate.
func (m *SettingsManager) secretData(ctx context.Context) (map[string][]byte, error) {
	secret, err := m.clientset.CoreV1().Secrets(m.namespace).Get(ctx, SecretName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return map[string][]byte{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get secret %s: %w", SecretName, err)
	}
	if secret.Data == nil {
		return map[string][]byte{}, nil
	}
	return secret.Data, nil
}

// configMapData returns the named ConfigMap's data, or an empty map if it
// does not exist. Only non-NotFound API errors propagate.
func (m *SettingsManager) configMapData(ctx context.Context, name string) (map[string]string, error) {
	cm, err := m.clientset.CoreV1().ConfigMaps(m.namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get configmap %s: %w", name, err)
	}
	if cm.Data == nil {
		return map[string]string{}, nil
	}
	return cm.Data, nil
}

// GetSigningKey returns the HS256 signing key from the dashboard Secret. It
// errors if the key is absent or shorter than MinSigningKeyLength.
func (m *SettingsManager) GetSigningKey(ctx context.Context) ([]byte, error) {
	data, err := m.secretData(ctx)
	if err != nil {
		return nil, err
	}
	key := data[KeyServerSignature]
	if len(key) < MinSigningKeyLength {
		return nil, fmt.Errorf("signing key %q is missing or shorter than %d bytes", KeyServerSignature, MinSigningKeyLength)
	}
	return key, nil
}
