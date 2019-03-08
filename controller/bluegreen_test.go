package controller

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"k8s.io/kubernetes/pkg/controller"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	core "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/annotations"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	"github.com/stretchr/testify/assert"
	"github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/fake"
)

var (
	noTimestamp = metav1.Time{}
)

func newBlueGreenRollout(name string, replicas int, revisionHistoryLimit *int32, activeSvc string, previewSvc string) *v1alpha1.Rollout {
	rollout := newRollout(name, replicas, revisionHistoryLimit, map[string]string{"foo": "bar"})
	rollout.Spec.Strategy.BlueGreenStrategy = &v1alpha1.BlueGreenStrategy{
		ActiveService:  activeSvc,
		PreviewService: previewSvc,
	}
	rollout.Status.CurrentStepHash = conditions.ComputeStepHash(rollout)
	rollout.Status.CurrentPodHash = controller.ComputeHash(&rollout.Spec.Template, rollout.Status.CollisionCount)
	return rollout
}

func newAvailableCondition(available bool) ([]v1alpha1.RolloutCondition, string) {
	message := "Rollout is not serving traffic from the active service."
	status := corev1.ConditionFalse
	if available {
		message = "Rollout is serving traffic from the active service."
		status = corev1.ConditionTrue

	}
	rc := []v1alpha1.RolloutCondition{{
		LastTransitionTime: metav1.Now(),
		LastUpdateTime:     metav1.Now(),
		Message:            message,
		Reason:             "Available",
		Status:             status,
		Type:               v1alpha1.RolloutAvailable,
	}}
	rcStr, _ := json.Marshal(rc)
	return rc, string(rcStr)
}

func TestBlueGreenHandlePreviewWhenActiveSet(t *testing.T) {
	f := newFixture(t)

	r1 := newBlueGreenRollout("foo", 1, nil, "preview", "active")

	r2 := r1.DeepCopy()
	annotations.SetRolloutRevision(r2, "2")
	r2.Spec.Template.Spec.Containers[0].Image = "foo/bar2.0"
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	rs1 := newReplicaSetWithStatus(r1, "foo-895c6c4f9", 1, 1)
	f.kubeobjects = append(f.kubeobjects, rs1)
	f.replicaSetLister = append(f.replicaSetLister, rs1)

	rs2 := newReplicaSetWithStatus(r2, "foo-6479c8f85c", 1, 1)
	f.kubeobjects = append(f.kubeobjects, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs2)

	previewSvc := newService("preview", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: "895c6c4f9"})
	f.kubeobjects = append(f.kubeobjects, previewSvc)

	activeSvc := newService("active", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: "6479c8f85c"})
	f.kubeobjects = append(f.kubeobjects, activeSvc)

	f.expectGetServiceAction(previewSvc)
	f.expectGetServiceAction(activeSvc)
	f.expectPatchServiceAction(previewSvc, "")
	f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))
}

func TestBlueGreenCreatesReplicaSet(t *testing.T) {
	f := newFixture(t)

	r := newBlueGreenRollout("foo", 1, nil, "bar", "")
	f.rolloutLister = append(f.rolloutLister, r)
	f.objects = append(f.objects, r)
	s := newService("bar", 80, nil)
	f.kubeobjects = append(f.kubeobjects, s)

	rs := newReplicaSet(r, "foo-895c6c4f9", 1)

	f.expectCreateReplicaSetAction(rs)
	f.expectGetServiceAction(s)
	f.expectPatchRolloutAction(r)
	f.run(getKey(r, t))
}

func TestBlueGreenSetPreviewService(t *testing.T) {
	f := newFixture(t)

	r := newBlueGreenRollout("foo", 1, nil, "active", "preview")
	f.rolloutLister = append(f.rolloutLister, r)
	f.objects = append(f.objects, r)

	rs := newReplicaSetWithStatus(r, "foo-895c6c4f9", 1, 1)
	f.kubeobjects = append(f.kubeobjects, rs)
	f.replicaSetLister = append(f.replicaSetLister, rs)

	previewSvc := newService("preview", 80, nil)
	selector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: "test"}
	activeSvc := newService("active", 80, selector)
	f.kubeobjects = append(f.kubeobjects, previewSvc, activeSvc)

	f.expectGetServiceAction(activeSvc)
	f.expectGetServiceAction(previewSvc)
	f.expectPatchServiceAction(previewSvc, "")
	f.expectPatchRolloutAction(r)
	f.run(getKey(r, t))
}

func TestBlueGreenHandlePause(t *testing.T) {
	t.Run("NoActionsWhilePaused", func(t *testing.T) {
		f := newFixture(t)
		f.checkObjects = true

		r1 := newBlueGreenRollout("foo", 1, nil, "active", "preview")
		r2 := bumpVersion(r1)

		rs1 := newReplicaSetWithStatus(r1, "foo-895c6c4f9", 1, 1)
		rs2 := newReplicaSetWithStatus(r2, "foo-5f79b78d7f", 1, 1)
		rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
		rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

		r2 = updateBlueGreenRolloutStatus(r2, rs2PodHash, rs1PodHash, 2,1,1,true,true)

		previewSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}
		previewSvc := newService("preview", 80, previewSelector)
		activeSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}
		activeSvc := newService("active", 80, activeSelector)

		f.objects = append(f.objects, r2)
		f.kubeobjects = append(f.kubeobjects, previewSvc, activeSvc, rs1, rs2)
		f.rolloutLister = append(f.rolloutLister, r2)
		f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

		f.expectGetServiceAction(activeSvc)
		f.expectGetServiceAction(previewSvc)
		f.expectPatchRolloutActionWithPatch(r2, OnlyObservedGenerationPatch)
		f.run(getKey(r2, t))
	})

	t.Run("SkipWhenNoPreviewSpecified", func(t *testing.T) {
		f := newFixture(t)
		f.checkObjects = true

		r1 := newBlueGreenRollout("foo", 1, nil, "active", "")
		r2 := bumpVersion(r1)

		rs1 := newReplicaSetWithStatus(r1, "foo-895c6c4f9", 1, 1)
		rs2 := newReplicaSetWithStatus(r2, "foo-5f79b78d7f", 1, 1)
		rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
		rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

		r2 = updateBlueGreenRolloutStatus(r2, "", rs1PodHash, 2,1,1,false,true)

		availableConditions, _ := newAvailableCondition(true)
		r2.Status.Conditions = availableConditions
		r2.Status.BlueGreen.ActiveSelector = rs1PodHash

		activeSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}
		activeSvc := newService("active", 80, activeSelector)

		f.objects = append(f.objects, r2)
		f.kubeobjects = append(f.kubeobjects, activeSvc, rs1, rs2)
		f.rolloutLister = append(f.rolloutLister, r2)
		f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

		f.expectGetServiceAction(activeSvc)
		f.expectPatchServiceAction(activeSvc, rs2PodHash)
		expectedPatchWithoutSubs := `{
			"status": {
				"blueGreen": {
					"activeSelector": "%s"
				}
			}
		}`
		expectedPatch := fmt.Sprintf(expectedPatchWithoutSubs, rs2PodHash)
		f.expectPatchRolloutActionWithPatch(r2, expectedPatch)
		f.run(getKey(r2, t))
	})

	t.Run("SkipNoActiveSelector", func(t *testing.T) {
		f := newFixture(t)
		f.checkObjects = true

		r1 := newBlueGreenRollout("foo", 1, nil, "active", "preview")

		rs1 := newReplicaSetWithStatus(r1, "foo-895c6c4f9", 1, 1)
		rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

		r1 = updateBlueGreenRolloutStatus(r1, "", "", 1,1,1,false,false)

		activeSvc := newService("active", 80, nil)
		previewSvc := newService("preview", 80, nil)

		f.objects = append(f.objects, r1)
		f.kubeobjects = append(f.kubeobjects, activeSvc, previewSvc, rs1)
		f.rolloutLister = append(f.rolloutLister, r1)
		f.replicaSetLister = append(f.replicaSetLister, rs1)

		f.expectGetServiceAction(activeSvc)
		f.expectGetServiceAction(previewSvc)
		f.expectPatchServiceAction(activeSvc, rs1PodHash)
		expectedPatchWithoutSubs := `{
			"status": {
				"blueGreen": {
					"activeSelector": "%s"
				}
			}
		}`
		expectedPatch := fmt.Sprintf(expectedPatchWithoutSubs, rs1PodHash)
		f.expectPatchRolloutActionWithPatch(r1, expectedPatch)
		f.run(getKey(r1, t))
	})

	t.Run("ContinueAfterUnpaused", func(t *testing.T) {
		f := newFixture(t)
		f.checkObjects = true

		r1 := newBlueGreenRollout("foo", 1, nil, "active", "preview")
		r2 := bumpVersion(r1)

		rs1 := newReplicaSetWithStatus(r1, "foo-895c6c4f9", 1, 1)
		rs2 := newReplicaSetWithStatus(r2, "foo-5f79b78d7f", 1, 1)
		rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
		rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

		r2 = updateBlueGreenRolloutStatus(r2, rs2PodHash, rs1PodHash, 2,1,1,false,true)
		now := metav1.Now()
		r2.Status.PauseStartTime = &now

		activeSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}
		activeSvc := newService("active", 80, activeSelector)
		previewSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}
		previewSvc := newService("preview", 80, previewSelector)

		f.objects = append(f.objects, r2)
		f.kubeobjects = append(f.kubeobjects, activeSvc, previewSvc, rs1, rs2)
		f.rolloutLister = append(f.rolloutLister, r2)
		f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

		f.expectGetServiceAction(activeSvc)
		f.expectGetServiceAction(previewSvc)
		f.expectPatchServiceAction(activeSvc, rs2PodHash)
		expectedPatchWithoutSubs := `{
			"status": {
				"blueGreen": {
					"activeSelector": "%s"
				},
				"pauseStartTime": null
			}
		}`
		expectedPatch := fmt.Sprintf(expectedPatchWithoutSubs, rs2PodHash)
		f.expectPatchRolloutActionWithPatch(r2, expectedPatch)
		f.run(getKey(r2, t))
	})
}

func TestBlueGreenSkipPreviewUpdateActive(t *testing.T) {
	f := newFixture(t)

	r := newBlueGreenRollout("foo", 1, nil, "active", "preview")
	r.Status.AvailableReplicas = 1
	f.rolloutLister = append(f.rolloutLister, r)
	f.objects = append(f.objects, r)

	rs := newReplicaSetWithStatus(r, "foo-895c6c4f9", 1, 1)
	f.kubeobjects = append(f.kubeobjects, rs)
	f.replicaSetLister = append(f.replicaSetLister, rs)

	previewSvc := newService("preview", 80, nil)
	activeSvc := newService("active", 80, nil)
	f.kubeobjects = append(f.kubeobjects, previewSvc, activeSvc)

	f.expectGetServiceAction(activeSvc)
	f.expectGetServiceAction(previewSvc)
	f.expectPatchServiceAction(activeSvc, rs.Labels[v1alpha1.DefaultRolloutUniqueLabelKey])
	f.expectPatchRolloutAction(r)
	f.run(getKey(r, t))
}

func TestBlueGreenScaleDownOldRS(t *testing.T) {
	f := newFixture(t)

	r1 := newBlueGreenRollout("foo", 1, nil, "bar", "")

	r2 := bumpVersion(r1)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	rs1 := newReplicaSetWithStatus(r1, "foo-895c6c4f9", 1, 1)
	f.kubeobjects = append(f.kubeobjects, rs1)
	f.replicaSetLister = append(f.replicaSetLister, rs1)

	rs2 := newReplicaSetWithStatus(r2, "foo-5f79b78d7f", 1, 1)
	f.kubeobjects = append(f.kubeobjects, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs2)
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	serviceSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}
	s := newService("bar", 80, serviceSelector)
	f.kubeobjects = append(f.kubeobjects, s)

	expRS := rs2.DeepCopy()
	expRS.Annotations[annotations.DesiredReplicasAnnotation] = "0"
	f.expectGetServiceAction(s)
	f.expectUpdateReplicaSetAction(expRS)
	f.expectPatchRolloutAction(r1)

	f.run(getKey(r2, t))
}

func TestBlueGreenRolloutStatusHPAStatusFieldsActiveSelectorSet(t *testing.T) {
	ro := newBlueGreenRollout("foo", 1, nil, "active", "preview")
	rs1 := newReplicaSetWithStatus(ro, "foo-867bc46cdc", 1, 1)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	ro2 := bumpVersion(ro)
	rs2 := newReplicaSetWithStatus(ro2, "foo-5f79b78d7f", 1, 1)
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	previewSvc := newService("preview", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash})
	activeSvc := newService("active", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash})
	ro2.Status.BlueGreen.PreviewSelector = rs2PodHash
	ro2.Status.BlueGreen.ActiveSelector = rs1PodHash
	ro2.Spec.Paused = true
	now := metav1.Now()
	ro2.Status.PauseStartTime = &now

	f := newFixture(t)
	f.rolloutLister = append(f.rolloutLister, ro2)
	f.objects = append(f.objects, ro2)

	f.kubeobjects = append(f.kubeobjects, rs1, rs2, previewSvc, activeSvc)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

	f.expectGetServiceAction(activeSvc)
	f.expectGetServiceAction(previewSvc)
	f.expectPatchRolloutAction(ro)
	f.run(getKey(ro, t))
	result := filterInformerActions(f.client.Actions())[0].(core.PatchAction).GetPatch()
	expectedPatchWithoutTimeStamps := calculatePatch(ro2, `{
		"status":{
			"HPAReplicas":1,
			"availableReplicas":2,
			"updatedReplicas":1,
			"replicas":2,
			"conditions":[
				{
					"lastTransitionTime":"%s",
					"lastUpdateTime":"%s",
					"message":"Rollout is serving traffic from the active service.",
					"reason":"Available",
					"status":"True",
					"type":"Available"
				}
			],
			"selector":"foo=bar,rollouts-pod-template-hash=%s"
		}
	}`)
	nowStr := now.UTC().Format(time.RFC3339)
	expectedPatch := fmt.Sprintf(expectedPatchWithoutTimeStamps, nowStr, nowStr, rs1PodHash)
	assert.Equal(t, expectedPatch, string(result))
}

func TestBlueGreenRolloutStatusHPAStatusFieldsNoActiveSelector(t *testing.T) {
	ro := newBlueGreenRollout("foo", 2, nil, "active", "")
	rs := newReplicaSetWithStatus(ro, "foo-1", 1, 1)
	ro.Status.CurrentPodHash = rs.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	activeSvc := newService("active", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: ""})

	fake := fake.Clientset{}
	k8sfake := k8sfake.Clientset{}
	controller := &Controller{
		rolloutsclientset: &fake,
		kubeclientset:     &k8sfake,
		recorder:          &record.FakeRecorder{},
	}

	err := controller.syncRolloutStatusBlueGreen([]*appsv1.ReplicaSet{rs}, rs, nil, activeSvc, ro, false)
	assert.Nil(t, err)
	assert.Len(t, fake.Actions(), 1)
	result := fake.Actions()[0].(core.PatchAction).GetPatch()
	expectedPatchWithoutTimeStamps := calculatePatch(ro, `{
		"status":{
			"HPAReplicas":1,
			"availableReplicas": 1,
			"updatedReplicas":1,
			"replicas":1,
			"conditions":[
				{
					"lastTransitionTime":"%s",
					"lastUpdateTime":"%s",
					"message":"Rollout is not serving traffic from the active service.",
					"reason":"Available",
					"status":"False",
					"type":"Available"
				}
			],
			"selector":"foo=bar"
		}
	}`)
	now := metav1.Now().UTC().Format(time.RFC3339)
	expectedPatch := fmt.Sprintf(expectedPatchWithoutTimeStamps, now, now)
	assert.Equal(t, expectedPatch, string(result))
}
