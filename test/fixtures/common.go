package fixtures

import (
	"encoding/json"
	"testing"

	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	clientset "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned"
)

//nolint:structcheck
type Common struct {
	testInstanceID string
	t              *testing.T
	namespace      string
	log            *log.Entry
	kubeClient     kubernetes.Interface
	dynamicClient  dynamic.Interface
	rolloutClient  clientset.Interface

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

func (c *Common) printJSON(obj interface{}) {
	// print status
	bytes, err := json.Marshal(obj)
	c.CheckError(err)
	c.log.Debug("---")
	c.log.Debug(string(bytes))
	c.log.Debug("---")
}
