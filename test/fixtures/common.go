package fixtures

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/ghodss/yaml"
	"github.com/sirupsen/logrus"
	log "github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	rov1 "github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	clientset "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/get"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/viewcontroller"
	"github.com/argoproj/argo-rollouts/utils/annotations"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
	unstructuredutil "github.com/argoproj/argo-rollouts/utils/unstructured"
)

//nolint:structcheck
type Common struct {
	Context        context.Context
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

func (c *Common) PrintRollout(name string) {
	streams := genericclioptions.IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr}
	o := options.NewArgoRolloutsOptions(streams)
	getOptions := get.GetOptions{
		ArgoRolloutsOptions: *o,
	}
	controller := viewcontroller.NewRolloutViewController(c.namespace, name, c.kubeClient, c.rolloutClient)
	ctx := context.Background()
	controller.Start(ctx)
	ri, err := controller.GetRolloutInfo()
	c.CheckError(err)
	getOptions.PrintRollout(ri)
}

func (c *Common) PrintRolloutYAML(ro *rov1.Rollout) {
	ro = ro.DeepCopy()
	// declutter the output
	delete(ro.Annotations, "kubectl.kubernetes.io/last-applied-configuration")
	ro.ManagedFields = nil
	yamlBytes, err := yaml.Marshal(ro)
	c.CheckError(err)
	fmt.Fprintf(logrus.StandardLogger().Out, "\n---\n%s\n", string(yamlBytes))
}

func (c *Common) GetReplicaSetByRevision(revision string) *appsv1.ReplicaSet {
	selector, err := metav1.LabelSelectorAsSelector(c.Rollout().Spec.Selector)
	c.CheckError(err)

	findReplicaSet := func() *appsv1.ReplicaSet {
		replicasets, err := c.kubeClient.AppsV1().ReplicaSets(c.namespace).List(c.Context, metav1.ListOptions{LabelSelector: selector.String()})
		c.CheckError(err)
		var candidate *appsv1.ReplicaSet
		for i, rs := range replicasets.Items {
			if rs.Annotations[annotations.RevisionAnnotation] == revision {
				if candidate != nil {
					c.t.Fatalf("Found multiple ReplicaSets with revision: %s", revision)
				}
				candidate = &replicasets.Items[i]
			}
		}
		return candidate
	}
	// make several attempts since sometimes we can check too early
	var found *appsv1.ReplicaSet
	for i := 0; i < 4; i++ {
		found = findReplicaSet()
		if found != nil {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if found == nil {
		c.t.Fatalf("Could not find ReplicaSet with revision: %s", revision)
	}
	return found
}

func (c *Common) GetPodsByRevision(revision string) *corev1.PodList {
	rs := c.GetReplicaSetByRevision(revision)
	pods, err := replicasetutil.GetPodsOwnedByReplicaSet(c.Context, c.kubeClient, rs)
	c.CheckError(err)
	podList := corev1.PodList{}
	for _, pod := range pods {
		podList.Items = append(podList.Items, *pod)
	}
	return &podList
}

func (c *Common) GetRolloutAnalysisRuns() rov1.AnalysisRunList {
	aruns, err := c.rolloutClient.ArgoprojV1alpha1().AnalysisRuns(c.namespace).List(c.Context, metav1.ListOptions{})
	c.CheckError(err)
	// filter analysis runs by ones owned by rollout to allow test parallelism
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
	for i, arun := range aruns.Items {
		if arun.Labels[rov1.RolloutTypeLabel] != rov1.RolloutTypeBackgroundRunLabel {
			continue
		}
		if found != nil {
			c.log.Error("Found multiple background analysis runs")
			c.t.FailNow()
		}
		found = &aruns.Items[i]
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
	for i, arun := range aruns.Items {
		if arun.Labels[rov1.RolloutTypeLabel] != rov1.RolloutTypeStepLabel {
			continue
		}
		if latest == nil {
			latest = &arun
			continue
		}
		if arun.CreationTimestamp.After(latest.CreationTimestamp.Time) {
			latest = &aruns.Items[i]
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

func (c *Common) GetPrePromotionAnalysisRun() *rov1.AnalysisRun {
	aruns := c.GetRolloutAnalysisRuns()
	var found *rov1.AnalysisRun
	for i, arun := range aruns.Items {
		if arun.Labels[rov1.RolloutTypeLabel] != rov1.RolloutTypePrePromotionLabel {
			continue
		}
		if found != nil {
			c.log.Error("Found multiple pre-promotion analysis runs")
			c.t.FailNow()
		}
		found = &aruns.Items[i]
	}
	if found == nil {
		c.log.Error("Pre-promotion AnalysisRun not found")
		c.t.FailNow()
	}
	return found
}

func (c *Common) GetPostPromotionAnalysisRun() *rov1.AnalysisRun {
	aruns := c.GetRolloutAnalysisRuns()
	var found *rov1.AnalysisRun
	for i, arun := range aruns.Items {
		if arun.Labels[rov1.RolloutTypeLabel] != rov1.RolloutTypePostPromotionLabel {
			continue
		}
		if found != nil {
			c.log.Error("Found multiple post-promotion analysis runs")
			c.t.FailNow()
		}
		found = &aruns.Items[i]
	}
	if found == nil {
		c.log.Error("Post-promotion AnalysisRun not found")
		c.t.FailNow()
	}
	return found
}

// GetExperimentAnalysisRun returns the AnalysisRun associated with the experiment.
// Errors if there is more than one.
func (c *Common) GetExperimentAnalysisRun(exp *rov1.Experiment) *rov1.AnalysisRun {
	aruns, err := c.rolloutClient.ArgoprojV1alpha1().AnalysisRuns(c.namespace).List(c.Context, metav1.ListOptions{})
	c.CheckError(err)
	var found *rov1.AnalysisRun
	for i, ar := range aruns.Items {
		controllerRef := metav1.GetControllerOf(&ar)
		if controllerRef != nil && controllerRef.UID == exp.UID {
			if found != nil {
				c.log.Errorf("Found multiple AnalysisRuns associated with experiment: %s", exp.Name)
				c.t.FailNow()
			}
			found = &aruns.Items[i]
		}
	}
	if found == nil {
		c.log.Error("Experiment AnalysisRun not found")
		c.t.FailNow()
	}
	return found
}

func (c *Common) GetRolloutExperiments() rov1.ExperimentList {
	exps, err := c.rolloutClient.ArgoprojV1alpha1().Experiments(c.namespace).List(c.Context, metav1.ListOptions{})
	c.CheckError(err)
	// filter experiments by ones owned by rollout to allow test parallellism
	var newExps rov1.ExperimentList
	for _, exp := range exps.Items {
		controllerRef := metav1.GetControllerOf(&exp)
		if controllerRef != nil && controllerRef.Name == c.rollout.GetName() {
			newExps.Items = append(newExps.Items, exp)
		}
	}
	return newExps
}

func (c *Common) GetExperimentByRevision(revision string) *rov1.Experiment {
	expList := c.GetRolloutExperiments()
	var found *rov1.Experiment
	for i, exp := range expList.Items {
		if exp.Annotations[annotations.RevisionAnnotation] == revision {
			if found != nil {
				c.t.Fatalf("Found multiple Experiments with revision: %s", revision)
			}
			found = &expList.Items[i]
		}
	}
	if found == nil {
		c.t.Fatalf("Could not find Experiment with revision: %s", revision)
	}
	return found
}

// GetJobFromAnalysisRun returns the Job associated with the AnalysisRun.
// Errors if there is more than one.
func (c *Common) GetJobFromAnalysisRun(ar *rov1.AnalysisRun) *batchv1.Job {
	jobs, err := c.kubeClient.BatchV1().Jobs(c.namespace).List(c.Context, metav1.ListOptions{})
	c.CheckError(err)

	var found *batchv1.Job
	for i, job := range jobs.Items {
		controllerRef := metav1.GetControllerOf(&job)
		if controllerRef != nil && controllerRef.UID == ar.UID {
			if found != nil {
				c.t.Fatalf("Found multiple Jobs associated with AnalysisRun: %s", ar.Name)
			}
			found = &jobs.Items[i]
		}
	}
	if found == nil {
		c.t.Fatalf("Could not find Job from AnalysisRun: %s", ar.Name)
	}
	return found
}

func (c *Common) GetReplicaSetFromExperiment(exp *rov1.Experiment, templateName string) *appsv1.ReplicaSet {
	replicasets, err := c.kubeClient.AppsV1().ReplicaSets(c.namespace).List(c.Context, metav1.ListOptions{})
	c.CheckError(err)
	var found *appsv1.ReplicaSet
	for i, rs := range replicasets.Items {
		controllerRef := metav1.GetControllerOf(&rs)
		if controllerRef == nil || controllerRef.UID != exp.UID || rs.Annotations[rov1.ExperimentTemplateNameAnnotationKey] != templateName {
			continue
		}
		if found != nil {
			c.t.Fatalf("Found multiple ReplicaSet associated with experiment: %s, template: %s", exp.Name, templateName)
		}
		found = &replicasets.Items[i]
	}
	if found == nil {
		c.t.Fatalf("Could not find ReplicaSet from experiment: %s, template: %s", exp.Name, templateName)
	}
	return found
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

func (c *Common) deleteObject(kind, name string) {
	cmd := exec.Command("kubectl", "delete", kind, name)
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		c.log.Errorf("kubectl delete of %s/%s failed: %s", kind, name, out)
		c.t.FailNow()
	}
	c.log.Info(string(out))
}
