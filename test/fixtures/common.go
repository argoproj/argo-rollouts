package fixtures

import (
	"encoding/json"
	"testing"

	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

func (c *Common) GetRolloutAnalysisRuns() rov1.AnalysisRunList {
	aruns, err := c.rolloutClient.ArgoprojV1alpha1().AnalysisRuns(c.namespace).List(metav1.ListOptions{})
	c.CheckError(err)
	// filter analysis runs by ones owned by rollout to allow test parallelism
	var newAruns rov1.AnalysisRunList
	for _, ar := range aruns.Items {
		controllerRef := metav1.GetControllerOf(&ar)
		if controllerRef != nil && controllerRef.Name == c.rollout.Name {
			newAruns.Items = append(newAruns.Items, ar)
		}
	}
	return newAruns
}

func (c *Common) GetBackgroundAnalysisRun() *rov1.AnalysisRun {
	aruns := c.GetRolloutAnalysisRuns()
	var found *rov1.AnalysisRun
	for _, arun := range aruns.Items {
		if arun.Labels[rov1.RolloutTypeLabel] != rov1.RolloutTypeBackgroundRunLabel {
			continue
		}
		if found != nil {
			c.log.Error("Found multiple background analysis runs")
			c.t.FailNow()
		}
		found = &arun
	}
	if found == nil {
		c.log.Error("Background AnalysisRun not found")
		c.t.FailNow()
	}
	return found
}

// GetInlineAnalysisRun returns the latest Step analysis run. This should generally be coupled with
// a count check, to ensure we are not checking the previous one. This may fail to accurately return
// the latest if the creationTimestamps are the same
func (c *Common) GetInlineAnalysisRun() *rov1.AnalysisRun {
	aruns := c.GetRolloutAnalysisRuns()
	var latest *rov1.AnalysisRun
	for _, arun := range aruns.Items {
		if arun.Labels[rov1.RolloutTypeLabel] != rov1.RolloutTypeStepLabel {
			continue
		}
		if latest == nil {
			latest = &arun
			continue
		}
		if arun.CreationTimestamp.After(latest.CreationTimestamp.Time) {
			latest = &arun
		}
		if arun.CreationTimestamp.Equal(&latest.CreationTimestamp) {
			c.log.Warnf("Found multiple inline analysis runs with same creationTimestamp: %s, %s", arun.Name, latest.Name)
		}
	}
	if latest == nil {
		c.log.Error("Inline AnalysisRun not found")
		c.t.FailNow()
	}
	return latest
}
