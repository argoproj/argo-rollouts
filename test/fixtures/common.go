package fixtures

import (
	"encoding/json"
	"testing"

	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	rov1 "github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	clientset "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned"
)

//nolint:structcheck
type Common struct {
	testInstanceID string
	t              *testing.T
	namespace      string
	log            *log.Entry
	kubernetesHost string
	kubeClient     kubernetes.Interface
	dynamicClient  dynamic.Interface
	rolloutClient  clientset.Interface

	rollout *rov1.Rollout
	objects []*unstructured.Unstructured
	// podDelay slows down pod startup and shutdown by the value in seconds
	// Used humans slow down rollout activity during a test
	podDelay int
}

func (c *Common) CheckError(err error) {
	c.t.Helper()
	if err != nil {
		c.t.Fatal(err)
	}
}

func (c *Common) PrintRollout(ro *rov1.Rollout) {
	// clean up output
	ro.ManagedFields = nil
	delete(ro.Annotations, "kubectl.kubernetes.io/last-applied-configuration")
	bytes, err := json.Marshal(ro)
	c.CheckError(err)
	c.log.Info(string(bytes))
}
