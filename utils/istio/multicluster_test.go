package istio

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

var (
	secret0                    = makeSecret("secret0", "namespace0", "primary0", []byte("kubeconfig0-0"))
	secret1                    = makeSecret("secret1", "namespace1", "primary1", []byte("kubeconfig1-1"))
	rolloutControllerNamespace = "argo-rollout-ns"
)

func TestGetPrimaryClusterDynamicClient(t *testing.T) {
	testCases := []struct {
		name              string
		namespace         string
		existingSecrets   []*v1.Secret
		expectedClusterId string
	}{
		{
			"TestNoPrimaryClusterSecret",
			metav1.NamespaceAll,
			nil,
			"",
		},
		{
			"TestPrimaryClusterSingleSecret",
			metav1.NamespaceAll,
			[]*v1.Secret{
				secret0,
			},
			"primary0",
		},
		{
			"TestPrimaryClusterMultipleSecrets",
			metav1.NamespaceAll,
			[]*v1.Secret{
				secret0,
				secret1,
			},
			"primary0",
		},
		{
			"TestPrimaryClusterNoSecretInNamespaceForNamespacedController",
			rolloutControllerNamespace,
			[]*v1.Secret{
				secret0,
			},
			"",
		},
		{
			"TestPrimaryClusterSingleSecretInNamespaceForNamespacedController",
			rolloutControllerNamespace,
			[]*v1.Secret{
				makeSecret("secret0", rolloutControllerNamespace, "primary0", []byte("kubeconfig0-0")),
			},
			"primary0",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var existingObjs []runtime.Object
			for _, s := range tc.existingSecrets {
				existingObjs = append(existingObjs, s)
			}

			client := fake.NewSimpleClientset(existingObjs...)
			clusterId, _ := GetPrimaryClusterDynamicClient(client, tc.namespace)
			assert.Equal(t, tc.expectedClusterId, clusterId)
		})
	}
}

func makeSecret(secret, namespace, clusterID string, kubeconfig []byte) *v1.Secret {
	return &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secret,
			Namespace: namespace,
			Labels: map[string]string{
				PrimaryClusterSecretLabel: "true",
			},
		},
		Data: map[string][]byte{
			clusterID: kubeconfig,
		},
	}
}
