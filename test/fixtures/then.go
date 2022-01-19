package fixtures

import (
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	"github.com/argoproj/argo-rollouts/experiments"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	rov1 "github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
	rolloututil "github.com/argoproj/argo-rollouts/utils/rollout"
	"github.com/stretchr/testify/assert"
)

type Then struct {
	*Common
}

func (t *Then) Assert(assertFunc func(t *Then)) *Then {
	assertFunc(t)
	return t
}

type RolloutExpectation func(*rov1.Rollout) bool

func (t *Then) ExpectRollout(expectation string, expectFunc RolloutExpectation) *Then {
	ro, err := t.rolloutClient.ArgoprojV1alpha1().Rollouts(t.namespace).Get(t.Context, t.rollout.GetName(), metav1.GetOptions{})
	t.CheckError(err)
	if !expectFunc(ro) {
		t.log.Errorf("Rollout expectation '%s' failed", expectation)
		t.t.FailNow()
	}
	t.log.Infof("Rollout expectation '%s' met", expectation)
	return t
}

func (t *Then) ExpectRolloutStatus(expectedStatus string) *Then {
	ro, err := t.rolloutClient.ArgoprojV1alpha1().Rollouts(t.namespace).Get(t.Context, t.rollout.GetName(), metav1.GetOptions{})
	t.CheckError(err)
	status, _ := rolloututil.GetRolloutPhase(ro)
	if string(status) != expectedStatus {
		t.log.Errorf("Rollout status expected to be '%s'. actual: %s", expectedStatus, status)
		t.t.FailNow()
	}
	t.log.Infof("Rollout expectation status=%s met", expectedStatus)
	return t
}

func (t *Then) ExpectReplicaCounts(desired, current, updated, ready, available interface{}) *Then {
	ro, err := t.rolloutClient.ArgoprojV1alpha1().Rollouts(t.namespace).Get(t.Context, t.rollout.GetName(), metav1.GetOptions{})
	t.CheckError(err)
	if desired != nil && desired.(int) != int(defaults.GetReplicasOrDefault(ro.Spec.Replicas)) {
		t.t.Fatalf("Expected %d desired replicas. Actual: %d", desired, defaults.GetReplicasOrDefault(ro.Spec.Replicas))
	}
	if current != nil && current.(int) != int(ro.Status.Replicas) {
		t.t.Fatalf("Expected %d current replicas. Actual: %d", current, ro.Status.Replicas)
	}
	if ready != nil && ready.(int) != int(ro.Status.ReadyReplicas) {
		t.t.Fatalf("Expected %d ready replicas. Actual: %d", ready, ro.Status.ReadyReplicas)
	}
	if updated != nil && updated.(int) != int(ro.Status.UpdatedReplicas) {
		t.t.Fatalf("Expected %d updated replicas. Actual: %d", updated, ro.Status.UpdatedReplicas)
	}
	if available != nil && available.(int) != int(ro.Status.AvailableReplicas) {
		t.t.Fatalf("Expected %d available replicas. Actual: %d", available, ro.Status.AvailableReplicas)
	}
	t.log.Infof("Replica count expectation met (desired:%v, current:%v, updated:%v, ready:%v, available:%v)", desired, current, updated, ready, available)
	return t
}

type PodExpectation func(*corev1.PodList) bool
type ReplicasetExpectation func(*appsv1.ReplicaSet) bool

func (t *Then) ExpectPods(expectation string, expectFunc PodExpectation) *Then {
	t.t.Helper()
	selector, err := metav1.LabelSelectorAsSelector(t.Rollout().Spec.Selector)
	t.CheckError(err)
	pods, err := t.kubeClient.CoreV1().Pods(t.namespace).List(t.Context, metav1.ListOptions{LabelSelector: selector.String()})
	t.CheckError(err)
	if !expectFunc(pods) {
		t.log.Errorf("Pod expectation '%s' failed", expectation)
		t.t.FailNow()
	}
	t.log.Infof("Pod expectation '%s' met", expectation)
	return t
}

func (t *Then) ExpectRevisionPods(expectation string, revision string, expectFunc PodExpectation) *Then {
	t.t.Helper()
	pods := t.GetPodsByRevision(revision)
	if !expectFunc(pods) {
		t.log.Errorf("Pod expectation '%s' failed", expectation)
		t.t.FailNow()
	}
	t.log.Infof("Pod expectation '%s' met", expectation)
	return t
}

func (t *Then) ExpectCanaryStablePodCount(canaryCount, stableCount int) *Then {
	t.t.Helper()
	ro, err := t.rolloutClient.ArgoprojV1alpha1().Rollouts(t.namespace).Get(t.Context, t.rollout.GetName(), metav1.GetOptions{})
	t.CheckError(err)
	return t.expectPodCountByHash("canary", ro.Status.CurrentPodHash, canaryCount).
		expectPodCountByHash("stable", ro.Status.StableRS, stableCount)
}

func (t *Then) ExpectRevisionPodCount(revision string, expectedCount int) *Then {
	t.t.Helper()
	rs := t.GetReplicaSetByRevision(revision)
	description := fmt.Sprintf("revision:%s", revision)
	hash := rs.Labels[rov1.DefaultRolloutUniqueLabelKey]
	return t.expectPodCountByHash(description, hash, expectedCount)
}

func (t *Then) ExpectRevisionScaleDown(revision string, expectScaleDown bool) *Then {
	t.t.Helper()
	rs := t.GetReplicaSetByRevision(revision)
	description := fmt.Sprintf("revision:%s", revision)
	return t.expectRSScaleDownByName(description, rs.Name, expectScaleDown)
}

func (t *Then) expectRSScaleDownByName(description, name string, expectScaleDown bool) *Then {
	return t.ExpectRS(fmt.Sprintf("RS %s scale down", name), name, func(rs *appsv1.ReplicaSet) bool {
		hasScaleDownDelay := false

		if _, ok := rs.Annotations[v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey]; ok {
			hasScaleDownDelay = true
		}

		metExpectation := hasScaleDownDelay == expectScaleDown
		if !metExpectation {
			t.log.Warnf("unexpected %s (rs %s): expected to be scaled down is %v", description, name, expectScaleDown)
		}
		return metExpectation
	})
}

func (t *Then) ExpectRS(expectation string, name string, expectFunc ReplicasetExpectation) *Then {
	t.t.Helper()
	_, err := metav1.LabelSelectorAsSelector(t.Rollout().Spec.Selector)
	t.CheckError(err)
	rs, err := t.kubeClient.AppsV1().ReplicaSets(t.namespace).Get(t.Context, name, metav1.GetOptions{})
	// List(t.Context, metav1.ListOptions{LabelSelector: selector.String()})
	t.CheckError(err)
	if !expectFunc(rs) {
		t.log.Errorf("rs expectation '%s' failed", expectation)
		t.t.FailNow()
	}
	t.log.Infof("rs expectation '%s' met", expectation)
	return t
}

func (t *Then) expectPodCountByHash(description, hash string, expectedCount int) *Then {
	return t.ExpectPods(fmt.Sprintf("%s pod count == %d", description, expectedCount), func(pods *corev1.PodList) bool {
		count := 0
		for _, pod := range pods.Items {
			if pod.DeletionTimestamp != nil {
				// ignore deleting pods from the count, since it messes with the counts and will
				// disappear soon anyways.
				t.log.Debugf("ignoring deleting pod %s from expected pod count", pod.Name)
				continue
			}
			if pod.Labels[rov1.DefaultRolloutUniqueLabelKey] == hash {
				count += 1
			}
		}
		metExpectation := expectedCount == count
		if !metExpectation {
			t.log.Warnf("unexpected %s (hash %s) pod count: expected %d, saw: %d", description, hash, expectedCount, count)
		}
		return metExpectation
	})
}

type ReplicaSetExpectation func(*appsv1.ReplicaSetList) bool

func (t *Then) ExpectReplicaSets(expectation string, expectFunc ReplicaSetExpectation) *Then {
	t.t.Helper()
	selector, err := metav1.LabelSelectorAsSelector(t.Rollout().Spec.Selector)
	t.CheckError(err)
	replicasets, err := t.kubeClient.AppsV1().ReplicaSets(t.namespace).List(t.Context, metav1.ListOptions{LabelSelector: selector.String()})
	t.CheckError(err)
	if !expectFunc(replicasets) {
		t.log.Errorf("Replicaset expectation '%s' failed", expectation)
		t.t.FailNow()
	}
	t.log.Infof("Replicaset expectation '%s' met", expectation)
	return t
}

type ExperimentTemplateReplicaSetExpectation func(set *appsv1.ReplicaSet) bool

func (t *Then) ExpectExperimentTemplateReplicaSet(expectation string, experiment string, template string, expectFunc ExperimentTemplateReplicaSetExpectation) *Then {
	ex, err := t.rolloutClient.ArgoprojV1alpha1().Experiments(t.namespace).Get(t.Context, experiment, metav1.GetOptions{})
	t.CheckError(err)
	rs := t.GetReplicaSetFromExperiment(ex, template)
	if !expectFunc(rs) {
		t.log.Errorf("Experiment template replicaset '%s' expectation '%s' failed", rs.Name, expectation)
		t.t.FailNow()
	}
	t.log.Infof("Experiment template replicaset '%s' expectation '%s' met", rs.Name, expectation)
	return t
}

func (t *Then) ExpectExperimentDryRunSummary(expectedCount, expectedErrorCount, expectedFailureCount int32, experiment string) *Then {
	expectation := v1alpha1.RunSummary{
		Count:  expectedCount,
		Error:  expectedErrorCount,
		Failed: expectedFailureCount,
	}
	t.log.Infof("Expected Dry-Run Summary: Count=%d, Successful=%d, Failed=%d, Error=%d, Inconclusive=%d", expectation.Count, expectation.Successful, expectation.Failed, expectation.Error, expectation.Inconclusive)
	ex, err := t.rolloutClient.ArgoprojV1alpha1().Experiments(t.namespace).Get(t.Context, experiment, metav1.GetOptions{})
	t.CheckError(err)
	ar := t.GetExperimentAnalysisRun(ex)
	dryRunSummary := ar.Status.DryRunSummary
	if dryRunSummary != nil {
		t.log.Infof("Analysis Dry-Run Summary: Count=%d, Successful=%d, Failed=%d, Error=%d, Inconclusive=%d", dryRunSummary.Count, dryRunSummary.Successful, dryRunSummary.Failed, dryRunSummary.Error, dryRunSummary.Inconclusive)
		if expectation == *dryRunSummary {
			t.log.Infof("Expectation Matches!")
		} else {
			t.log.Errorf("Dry-Run Summary of AnalysisRun: '%s' doesn't match the expectations", ar.Name)
			t.t.FailNow()
		}
	} else {
		t.log.Errorf("Dry-Run Summary not found in the AnalysisRun: '%s'", ar.Name)
		t.t.FailNow()
	}
	return t
}

func (t *Then) ExpectExperimentMeasurementsLength(metricResultsIndex, expectedMeasurementsLength int, experiment string) *Then {
	t.log.Infof("Expected Measurements Length '%d' for MetricResults index '%d'", expectedMeasurementsLength, metricResultsIndex)
	ex, err := t.rolloutClient.ArgoprojV1alpha1().Experiments(t.namespace).Get(t.Context, experiment, metav1.GetOptions{})
	t.CheckError(err)
	ar := t.GetExperimentAnalysisRun(ex)
	if len(ar.Status.MetricResults) <= metricResultsIndex {
		t.log.Errorf("MetricResults Array doesn't have given index '%d' in the AnalysisRun: '%s'", metricResultsIndex, ar.Name)
		t.t.FailNow()
	}
	measurementsLength := len(ar.Status.MetricResults[metricResultsIndex].Measurements)
	t.log.Infof("Actual Measurements Length at index '%d': '%d'", metricResultsIndex, measurementsLength)
	if measurementsLength == expectedMeasurementsLength {
		t.log.Infof("Expectation Matches!")
	} else {
		t.log.Errorf("Measurements Length at index '%d' of AnalysisRun: '%s' doesn't match the expectations", metricResultsIndex, ar.Name)
		t.t.FailNow()
	}
	return t
}

func (t *Then) ExpectExperimentTemplateReplicaSetNumReplicas(experiment string, template string, expectedReplicas int) *Then {
	return t.ExpectExperimentTemplateReplicaSet(fmt.Sprintf("experiment template '%s' num replicas == %d", template, expectedReplicas), experiment, template, func(rs *appsv1.ReplicaSet) bool {
		return int(rs.Status.Replicas) == expectedReplicas
	})
}

type AnalysisRunListExpectation func(*rov1.AnalysisRunList) bool
type AnalysisRunExpectation func(*rov1.AnalysisRun) bool

func (t *Then) ExpectAnalysisRuns(expectation string, expectFunc AnalysisRunListExpectation) *Then {
	t.t.Helper()
	aruns := t.GetRolloutAnalysisRuns()
	if !expectFunc(&aruns) {
		t.log.Errorf("AnalysisRun expectation '%s' failed", expectation)
		t.t.FailNow()
	}
	t.log.Infof("AnalysisRun expectation '%s' met", expectation)
	return t
}

func (t *Then) ExpectAnalysisRunCount(expectedCount int) *Then {
	t.t.Helper()
	return t.ExpectAnalysisRuns(fmt.Sprintf("analysisrun count == %d", expectedCount), func(aruns *rov1.AnalysisRunList) bool {
		return len(aruns.Items) == expectedCount
	})
}

func (t *Then) ExpectBackgroundAnalysisRun(expectation string, expectFunc AnalysisRunExpectation) *Then {
	t.t.Helper()
	bgArun := t.GetBackgroundAnalysisRun()
	if !expectFunc(bgArun) {
		t.log.Errorf("Background AnalysisRun expectation '%s' failed", expectation)
		t.t.FailNow()
	}
	t.log.Infof("Background AnalysisRun expectation '%s' met", expectation)
	return t
}

func (t *Then) ExpectBackgroundAnalysisRunPhase(phase string) *Then {
	t.t.Helper()
	return t.ExpectBackgroundAnalysisRun(fmt.Sprintf("background analysis phase == %s", phase),
		func(run *rov1.AnalysisRun) bool {
			return string(run.Status.Phase) == phase
		},
	)
}

func (t *Then) ExpectInlineAnalysisRun(expectation string, expectFunc AnalysisRunExpectation) *Then {
	t.t.Helper()
	bgArun := t.GetInlineAnalysisRun()
	if !expectFunc(bgArun) {
		t.log.Errorf("Inline AnalysisRun expectation '%s' failed", expectation)
		t.t.FailNow()
	}
	t.log.Infof("Inline AnalysisRun expectation '%s' met", expectation)
	return t
}

func (t *Then) ExpectInlineAnalysisRunPhase(phase string) *Then {
	t.t.Helper()
	return t.ExpectInlineAnalysisRun(fmt.Sprintf("inline analysis phase == %s", phase),
		func(run *rov1.AnalysisRun) bool {
			return string(run.Status.Phase) == phase
		},
	)
}

// ExpectStableRevision verifies the ReplicaSet with the specified revision is marked stable
func (t *Then) ExpectStableRevision(revision string) *Then {
	t.t.Helper()
	verifyRevision := func() error {
		ro, err := t.rolloutClient.ArgoprojV1alpha1().Rollouts(t.namespace).Get(t.Context, t.rollout.GetName(), metav1.GetOptions{})
		t.CheckError(err)
		rs := t.GetReplicaSetByRevision(revision)
		if replicasetutil.GetPodTemplateHash(rs) != ro.Status.StableRS {
			return fmt.Errorf("Expectation failed: stable %s != ReplicaSet revision %s (hash: %s)", ro.Status.StableRS, revision, replicasetutil.GetPodTemplateHash(rs))
		}
		return nil
	}
	err := verifyRevision()
	t.CheckError(err)
	return t
}

// ExpectPreviewRevision verifies the preview service selector is pointing to the specified revision
func (t *Then) ExpectPreviewRevision(revision string) *Then {
	t.t.Helper()
	return t.verifyBlueGreenSelectorRevision("preview", revision)
}

// ExpectActiveRevision verifies the active service selector is pointing to the specified revision
func (t *Then) ExpectActiveRevision(revision string) *Then {
	t.t.Helper()
	return t.verifyBlueGreenSelectorRevision("active", revision)
}

func (t *Then) verifyBlueGreenSelectorRevision(which string, revision string) *Then {
	verifyRevision := func() error {
		ro, err := t.rolloutClient.ArgoprojV1alpha1().Rollouts(t.namespace).Get(t.Context, t.rollout.GetName(), metav1.GetOptions{})
		t.CheckError(err)
		var serviceName, selector string
		switch which {
		case "active":
			serviceName = ro.Spec.Strategy.BlueGreen.ActiveService
			selector = ro.Status.BlueGreen.ActiveSelector
		case "preview":
			serviceName = ro.Spec.Strategy.BlueGreen.PreviewService
			selector = ro.Status.BlueGreen.PreviewSelector
		default:
			panic(fmt.Sprintf("unknown selector: %s", which))
		}
		svc, err := t.kubeClient.CoreV1().Services(t.namespace).Get(t.Context, serviceName, metav1.GetOptions{})
		t.CheckError(err)
		rs := t.GetReplicaSetByRevision(revision)
		if selector != svc.Spec.Selector[rov1.DefaultRolloutUniqueLabelKey] {
			return fmt.Errorf("Expectation failed: blueGreen %s selector/service selector mismatch %s != %s", which, selector, svc.Spec.Selector[rov1.DefaultRolloutUniqueLabelKey])
		}
		if selector != rs.Labels[rov1.DefaultRolloutUniqueLabelKey] {
			return fmt.Errorf("Expectation failed: blueGreen %s selector/replicaset label mismatch %s != %s", which, selector, rs.Labels[rov1.DefaultRolloutUniqueLabelKey])
		}
		return nil
	}
	// we perform several checks because switching the active service selector lags behind the Degraded/Promotion event
	var err error
	deadline := time.Now().Add(3 * time.Second)
	for {
		err = verifyRevision()
		if err == nil {
			t.log.Infof("Expectation: %s revision == '%s' met", which, revision)
			return t
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.log.Error(err)
	t.t.FailNow()
	return t
}

func (t *Then) ExpectServiceSelector(service string, selector map[string]string, ensurePodTemplateHash bool) *Then {
	t.t.Helper()
	svc, err := t.kubeClient.CoreV1().Services(t.namespace).Get(t.Context, service, metav1.GetOptions{})
	t.CheckError(err)
	if ensurePodTemplateHash {
		ro, err := t.rolloutClient.ArgoprojV1alpha1().Rollouts(t.namespace).Get(t.Context, t.rollout.GetName(), metav1.GetOptions{})
		t.CheckError(err)
		selector[rov1.DefaultRolloutUniqueLabelKey] = ro.Status.CurrentPodHash
	}
	if !reflect.DeepEqual(svc.Spec.Selector, selector) {
		t.t.Fatalf("Expected %s selector: %v. Actual: %v", service, selector, svc.Spec.Selector)
	}
	t.log.Infof("Expectation %s selector: %v met", service, selector)
	return t
}

type ExperimentServiceListExpectation func(map[string]*corev1.Service) bool
type ExperimentListExpectation func(*rov1.ExperimentList) bool
type ExperimentExpectation func(*rov1.Experiment) bool

func (t *Then) ExpectExperimentServices(expectation string, experiment string, expectFunc ExperimentServiceListExpectation) *Then {
	ex, err := t.rolloutClient.ArgoprojV1alpha1().Experiments(t.namespace).Get(t.Context, experiment, metav1.GetOptions{})
	t.CheckError(err)
	svcList, err := t.kubeClient.CoreV1().Services(t.namespace).List(t.Context, metav1.ListOptions{})
	t.CheckError(err)
	templateToService := make(map[string]*corev1.Service)
	for _, svc := range svcList.Items {
		svcBytes, err := json.Marshal(svc)
		t.CheckError(err)
		newSvc := &corev1.Service{}
		err = json.Unmarshal(svcBytes, newSvc)
		t.CheckError(err)
		err = experiments.GetServiceForExperiment(ex, newSvc, templateToService)
		t.CheckError(err)
	}
	if !expectFunc(templateToService) {
		t.log.Errorf("Experiment expectation '%s' failed", expectation)
		t.t.FailNow()
	}
	t.log.Infof("Experiment expectation '%s' met", expectation)
	return t
}

func (t *Then) ExpectExperimentServiceCount(experimentName string, expectedCount int) *Then {
	return t.ExpectExperimentServices(fmt.Sprintf("experiment services count == %d", expectedCount), experimentName, func(templateToService map[string]*corev1.Service) bool {
		count := 0
		for _, svc := range templateToService {
			if svc != nil {
				count++
			}
		}
		return count == expectedCount
	})
}

func (t *Then) ExpectExperiments(expectation string, expectFunc ExperimentListExpectation) *Then {
	exps := t.GetRolloutExperiments()
	if !expectFunc(&exps) {
		t.log.Errorf("Experiment expectation '%s' failed", expectation)
		t.t.FailNow()
	}
	t.log.Infof("Experiment expectation '%s' met", expectation)
	return t
}

func (t *Then) ExpectExperimentCount(expectedCount int) *Then {
	return t.ExpectExperiments(fmt.Sprintf("experiment count == %d", expectedCount), func(exps *rov1.ExperimentList) bool {
		return len(exps.Items) == expectedCount
	})
}

func (t *Then) ExpectExperimentByRevision(expectation string, revision string, expectFunc ExperimentExpectation) *Then {
	exp := t.GetExperimentByRevision(revision)
	if !expectFunc(exp) {
		t.log.Errorf("Experiment expectation '%s' failed", expectation)
		t.t.FailNow()
	}
	t.log.Infof("Experiment expectation '%s' met", expectation)
	return t
}

func (t *Then) ExpectExperimentByRevisionPhase(revision string, phase string) *Then {
	return t.ExpectExperimentByRevision(fmt.Sprintf("experiment rev:%s phase == %s", revision, phase), revision,
		func(run *rov1.Experiment) bool {
			return string(run.Status.Phase) == phase
		},
	)
}

func (t *Then) ExpectRolloutEvents(reasons []string) *Then {
	t.t.Helper()
	eventReasons := t.GetRolloutEventReasons()
	assert.Equal(t.Common.t, reasons, eventReasons)
	return t
}

func (t *Then) When() *When {
	return &When{
		Common: t.Common,
	}
}

func (t *Then) Given() *Given {
	return &Given{
		Common: t.Common,
	}
}
