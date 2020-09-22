package fixtures

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	rov1 "github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

type Then struct {
	Common
}

type RolloutExpectation func(*rov1.Rollout) bool

func (t *Then) ExpectRollout(expectation string, expectFunc RolloutExpectation) *Then {
	ro, err := t.rolloutClient.ArgoprojV1alpha1().Rollouts(t.namespace).Get(t.rollout.Name, metav1.GetOptions{})
	t.CheckError(err)
	if !expectFunc(ro) {
		t.log.Errorf("Rollout expectation '%s' failed", expectation)
		t.t.FailNow()
	}
	t.log.Infof("Rollout expectation '%s' met", expectation)
	return t
}

type PodExpectation func(*corev1.PodList) bool

func (t *Then) ExpectPods(expectation string, expectFunc PodExpectation) *Then {
	selector, err := metav1.LabelSelectorAsSelector(t.rollout.Spec.Selector)
	t.CheckError(err)
	pods, err := t.kubeClient.CoreV1().Pods(t.namespace).List(metav1.ListOptions{LabelSelector: selector.String()})
	t.CheckError(err)
	if !expectFunc(pods) {
		t.log.Errorf("Pod expectation '%s' failed", expectation)
		t.t.FailNow()
	}
	t.log.Infof("Pod expectation '%s' met", expectation)
	return t
}

func (t *Then) ExpectStablePodCount(expectedCount int) *Then {
	return t.expectPodCountByHash("stable", expectedCount)
}

func (t *Then) ExpectCanaryPodCount(expectedCount int) *Then {
	return t.expectPodCountByHash("canary", expectedCount)
}

func (t *Then) expectPodCountByHash(which string, expectedCount int) *Then {
	return t.ExpectPods(fmt.Sprintf("%s pod count == %d", which, expectedCount), func(pods *corev1.PodList) bool {
		ro, err := t.rolloutClient.ArgoprojV1alpha1().Rollouts(t.namespace).Get(t.rollout.Name, metav1.GetOptions{})
		t.CheckError(err)
		count := 0
		var hash string
		if which == "stable" {
			hash = ro.Status.Canary.StableRS
		} else if which == "canary" {
			hash = ro.Status.CurrentPodHash
		} else {
			t.t.Fatalf("unknown which: %s", which)
		}
		for _, pod := range pods.Items {
			if pod.DeletionTimestamp != nil {
				// ignore deleting pods from the count, since it messes with the counts and will
				// disappear soon anyways.
				t.log.Debugf("ignoring deleting pod %s from expected pod count", pod.Name)
				continue
			}
			if pod.Labels["rollouts-pod-template-hash"] == hash {
				count += 1
			}
		}
		metExpectation := expectedCount == count
		if !metExpectation {
			t.log.Warnf("unexpected %s pod count: expected %d, saw: %d", which, expectedCount, count)
		}
		return metExpectation
	})
}

type ReplicaSetExpectation func(*appsv1.ReplicaSetList) bool

func (t *Then) ExpectReplicaSets(expectation string, expectFunc ReplicaSetExpectation) *Then {
	selector, err := metav1.LabelSelectorAsSelector(t.rollout.Spec.Selector)
	t.CheckError(err)
	replicasets, err := t.kubeClient.AppsV1().ReplicaSets(t.namespace).List(metav1.ListOptions{LabelSelector: selector.String()})
	t.CheckError(err)
	if !expectFunc(replicasets) {
		t.log.Errorf("Replicaset expectation '%s' failed", expectation)
		t.t.FailNow()
	}
	t.log.Infof("Replicaset expectation '%s' met", expectation)
	return t
}

type AnalysisRunListExpectation func(*rov1.AnalysisRunList) bool
type AnalysisRunExpectation func(*rov1.AnalysisRun) bool

func (t *Then) ExpectAnalysisRuns(expectation string, expectFunc AnalysisRunListExpectation) *Then {
	aruns := t.GetRolloutAnalysisRuns()
	if !expectFunc(&aruns) {
		t.log.Errorf("AnalysisRun expectation '%s' failed", expectation)
		t.t.FailNow()
	}
	t.log.Infof("AnalysisRun expectation '%s' met", expectation)
	return t
}

func (t *Then) ExpectAnalysisRunCount(expectedCount int) *Then {
	return t.ExpectAnalysisRuns(fmt.Sprintf("analysisrun count == %d", expectedCount), func(aruns *rov1.AnalysisRunList) bool {
		return len(aruns.Items) == expectedCount
	})
}

func (t *Then) ExpectBackgroundAnalysisRun(expectation string, expectFunc AnalysisRunExpectation) *Then {
	bgArun := t.GetBackgroundAnalysisRun()
	if !expectFunc(bgArun) {
		t.log.Errorf("Background AnalysisRun expectation '%s' failed", expectation)
		t.t.FailNow()
	}
	t.log.Infof("Background AnalysisRun expectation '%s' met", expectation)
	return t
}

func (t *Then) ExpectBackgroundAnalysisRunPhase(phase string) *Then {
	return t.ExpectBackgroundAnalysisRun(fmt.Sprintf("background analysis phase == %s", phase),
		func(run *rov1.AnalysisRun) bool {
			return string(run.Status.Phase) == phase
		},
	)
}

func (t *Then) When() *When {
	return &When{
		Common: t.Common,
	}
}
