package fixtures

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"
	"testing"

	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	rov1 "github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	clientset "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned"
	unstructuredutil "github.com/argoproj/argo-rollouts/utils/unstructured"
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

	rollout *unstructured.Unstructured
	objects []*unstructured.Unstructured
}

func (c *Common) CheckError(err error) {
	c.t.Helper()
	if err != nil {
		c.t.Fatal(err)
	}
}

func (c *Common) Rollout() *rov1.Rollout {
	var ro rov1.Rollout
	err := runtime.DefaultUnstructuredConverter.FromUnstructured(c.rollout.Object, &ro)
	c.CheckError(err)
	return &ro
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
	// filter analysis runs by ones owned by rollout to allow test parallellism
	var newAruns rov1.AnalysisRunList
	for _, ar := range aruns.Items {
		controllerRef := metav1.GetControllerOf(&ar)
		if controllerRef != nil && controllerRef.Name == c.rollout.GetName() {
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

// ApplyManifests kubectl applys the given YAML string or file path:
// 1. A file name if it starts with "@"
// 2. Raw YAML.
func (c *Common) ApplyManifests(text string) {
	c.t.Helper()
	objs := c.parseTextToObjects(text)
	for _, obj := range objs {
		c.applyObject(obj)
	}
}

func (c *Common) parseTextToObjects(text string) []*unstructured.Unstructured {
	yamlBytes := c.yamlBytes(text)
	// Some E2E AnalysisTemplates use http://kubernetes.default.svc/version as a fake metric provider.
	// This doesn't work outside the cluster, so the following replaces it with the host from the
	// rest config.
	yamlString := strings.ReplaceAll(string(yamlBytes), "https://kubernetes.default.svc", c.kubernetesHost)
	objs, err := unstructuredutil.SplitYAML(yamlString)
	c.CheckError(err)
	return objs
}

func (c *Common) yamlBytes(text string) []byte {
	var yamlBytes []byte
	var err error
	if strings.HasPrefix(text, "@") {
		file := strings.TrimPrefix(text, "@")
		yamlBytes, err = ioutil.ReadFile(file)
		c.CheckError(err)
	} else {
		yamlBytes = []byte(text)
	}
	return yamlBytes
}

func (c *Common) applyObject(obj *unstructured.Unstructured) {
	labels := obj.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	if E2ELabelValueInstanceID != "" {
		labels[rov1.LabelKeyControllerInstanceID] = E2ELabelValueInstanceID
	}
	testNameSplit := strings.SplitN(c.t.Name(), "/", 2)
	if len(testNameSplit) > 1 {
		// when test name is the SuiteName/TestName
		labels[E2ELabelKeyTestName] = testNameSplit[1]
	} else {
		// when test name is the SuiteName
		labels[E2ELabelKeyTestName] = testNameSplit[0]
	}
	obj.SetLabels(labels)

	objBytes, err := json.Marshal(obj)
	c.CheckError(err)
	cmd := exec.Command("kubectl", "apply", "-f", "-")
	cmd.Env = os.Environ()
	cmd.Stdin = bytes.NewReader(objBytes)
	out, err := cmd.CombinedOutput()
	if err != nil {
		gvk := obj.GetObjectKind().GroupVersionKind()
		objMap, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
		un := unstructured.Unstructured{Object: objMap}
		c.log.Errorf("kubectl apply of %s %s failed: %s", gvk.Kind, un.GetName(), out)
		c.t.FailNow()
	}
	c.log.Info(string(out))
}
