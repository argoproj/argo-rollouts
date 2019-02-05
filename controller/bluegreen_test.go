package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/record"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/fake"
	"github.com/argoproj/argo-rollouts/utils/annotations"
)

var (
	noTimestamp = metav1.Time{}
)

func TestReconcileVerifyingPreview(t *testing.T) {
	boolPtr := func(boolean bool) *bool { return &boolean }
	tests := []struct {
		name                 string
		activeSvc            *corev1.Service
		previewSvcName       string
		verifyingPreviewFlag *bool
		notFinishedVerifying bool
	}{
		{
			name:                 "Continue if preview Service isn't specificed",
			activeSvc:            newService("active", 80, nil),
			verifyingPreviewFlag: boolPtr(true),
			notFinishedVerifying: false,
		},
		{
			name:                 "Continue if active service doesn't have a selector from the rollout",
			previewSvcName:       "previewSvc",
			activeSvc:            newService("active", 80, nil),
			verifyingPreviewFlag: boolPtr(true),
			notFinishedVerifying: false,
		},
		{
			name:                 "Do not continue if verifyingPreview flag is true",
			previewSvcName:       "previewSvc",
			activeSvc:            newService("active", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: "test"}),
			verifyingPreviewFlag: boolPtr(true),
			notFinishedVerifying: true,
		},
		{
			name:                 "Continue if verifyingPreview flag is false",
			previewSvcName:       "previewSvc",
			activeSvc:            newService("active", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: "test"}),
			verifyingPreviewFlag: boolPtr(false),
			notFinishedVerifying: false,
		},
		{
			name:                 "Continue if verifyingPreview flag is not set",
			previewSvcName:       "previewSvc",
			activeSvc:            newService("active", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: "test"}),
			notFinishedVerifying: false,
		},
	}
	for i := range tests {
		test := tests[i]
		t.Run(test.name, func(t *testing.T) {
			rollout := newBlueGreenRollout("foo", 1, nil, map[string]string{"foo": "bar"}, "", test.previewSvcName)
			rollout.Status = v1alpha1.RolloutStatus{
				VerifyingPreview: test.verifyingPreviewFlag,
			}
			fake := fake.Clientset{}
			k8sfake := k8sfake.Clientset{}
			controller := &Controller{
				rolloutsclientset: &fake,
				kubeclientset:     &k8sfake,
				recorder:          &record.FakeRecorder{},
			}
			finishedVerifying := controller.reconcileVerifyingPreview(test.activeSvc, rollout)
			assert.Equal(t, test.notFinishedVerifying, finishedVerifying)
		})
	}
}

func TestHandlePreviewWhenActiveSet(t *testing.T) {
	f := newFixture(t)

	r1 := newBlueGreenRollout("foo", 1, nil, map[string]string{"foo": "bar"}, "preview", "active")

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

func TestHandleVerifyingPreviewSetButNotPreviewSvc(t *testing.T) {
	f := newFixture(t)

	r1 := newBlueGreenRollout("foo", 1, nil, map[string]string{"foo": "bar"}, "active", "preview")
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

	previewSvc := newService("preview", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: ""})
	f.kubeobjects = append(f.kubeobjects, previewSvc)

	activeSvc := newService("active", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: "895c6c4f9"})
	f.kubeobjects = append(f.kubeobjects, activeSvc)

	r2.Status.VerifyingPreview = func(boolean bool) *bool { return &boolean }(true)

	f.expectGetServiceAction(previewSvc)
	f.expectGetServiceAction(activeSvc)
	f.expectPatchRolloutAction(r2)
	f.expectPatchServiceAction(previewSvc, "")
	f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))
}
