package controller

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	core "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"

	"github.com/argoproj/rollout-controller/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/rollout-controller/pkg/client/clientset/versioned/fake"
	"github.com/argoproj/rollout-controller/utils/annotations"
)

var (
	noTimestamp = metav1.Time{}
)

func TestController_reconcileVerifyingPreview(t *testing.T) {
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
			rollout := newRollout("foo", 1, nil, map[string]string{"foo": "bar"}, "", test.previewSvcName)
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

func TestController_reconcileNewReplicaSet(t *testing.T) {
	tests := []struct {
		name                string
		rolloutReplicas     int
		newReplicas         int
		scaleExpected       bool
		expectedNewReplicas int
	}{
		{
			name:            "New Replica Set matches rollout replica: No scale",
			rolloutReplicas: 10,
			newReplicas:     10,
			scaleExpected:   false,
		},
		{
			name:                "New Replica Set higher than rollout replica: Scale down",
			rolloutReplicas:     10,
			newReplicas:         12,
			scaleExpected:       true,
			expectedNewReplicas: 10,
		},
		{
			name:                "New Replica Set lower than rollout replica: Scale up",
			rolloutReplicas:     10,
			newReplicas:         8,
			scaleExpected:       true,
			expectedNewReplicas: 10,
		},
	}
	for i := range tests {
		test := tests[i]
		t.Run(test.name, func(t *testing.T) {
			test := tests[i]
			newRS := rs("foo-v2", test.newReplicas, nil, noTimestamp, nil)
			allRSs := []*appsv1.ReplicaSet{newRS}
			rollout := newRollout("foo", test.rolloutReplicas, nil, map[string]string{"foo": "bar"}, "", "")
			fake := fake.Clientset{}
			k8sfake := k8sfake.Clientset{}
			controller := &Controller{
				rolloutsclientset: &fake,
				kubeclientset:     &k8sfake,
				recorder:          &record.FakeRecorder{},
			}
			scaled, err := controller.reconcileNewReplicaSet(allRSs, newRS, rollout)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if !test.scaleExpected {
				if scaled || len(fake.Actions()) > 0 {
					t.Errorf("unexpected scaling: %v", fake.Actions())
				}
				return
			}
			if test.scaleExpected && !scaled {
				t.Errorf("expected scaling to occur")
				return
			}
			if len(k8sfake.Actions()) != 1 {
				t.Errorf("expected 1 action during scale, got: %v", fake.Actions())
				return
			}
			updated := k8sfake.Actions()[0].(core.UpdateAction).GetObject().(*appsv1.ReplicaSet)
			if e, a := test.expectedNewReplicas, int(*(updated.Spec.Replicas)); e != a {
				t.Errorf("expected update to %d replicas, got %d", e, a)
			}
		})
	}
}

func TestController_reconcileOldReplicaSet(t *testing.T) {
	tests := []struct {
		name                string
		rolloutReplicas     int
		oldReplicas         int
		newReplicas         int
		readyPodsFromOldRS  int
		readyPodsFromNewRS  int
		scaleExpected       bool
		expectedOldReplicas int
	}{
		{
			name:               "No pods to scale down",
			rolloutReplicas:    10,
			oldReplicas:        0,
			newReplicas:        10,
			readyPodsFromOldRS: 0,
			readyPodsFromNewRS: 0,
			scaleExpected:      false,
		},
		{
			name:               "New ReplicaSet is not fully healthy",
			rolloutReplicas:    10,
			oldReplicas:        10,
			newReplicas:        10,
			readyPodsFromOldRS: 10,
			readyPodsFromNewRS: 9,
			scaleExpected:      false,
		},
		{
			name:                "Clean up unhealthy pods",
			rolloutReplicas:     10,
			oldReplicas:         10,
			newReplicas:         10,
			readyPodsFromOldRS:  8,
			readyPodsFromNewRS:  10,
			scaleExpected:       true,
			expectedOldReplicas: 0,
		},
		{
			name:                "Normal scale down when new ReplicaSet is healthy",
			rolloutReplicas:     10,
			oldReplicas:         10,
			newReplicas:         10,
			readyPodsFromOldRS:  10,
			readyPodsFromNewRS:  10,
			scaleExpected:       true,
			expectedOldReplicas: 0,
		},
	}
	for i := range tests {
		test := tests[i]
		t.Run(test.name, func(t *testing.T) {
			newSelector := map[string]string{"foo": "new"}
			oldSelector := map[string]string{"foo": "old"}
			newRS := rs("foo-new", test.newReplicas, newSelector, noTimestamp, nil)
			newRS.Annotations = map[string]string{annotations.DesiredReplicasAnnotation: strconv.Itoa(test.newReplicas)}
			newRS.Status.AvailableReplicas = int32(test.readyPodsFromNewRS)
			oldRS := rs("foo-old", test.oldReplicas, oldSelector, noTimestamp, nil)
			oldRS.Annotations = map[string]string{annotations.DesiredReplicasAnnotation: strconv.Itoa(test.oldReplicas)}
			oldRS.Status.AvailableReplicas = int32(test.readyPodsFromOldRS)
			oldRSs := []*appsv1.ReplicaSet{oldRS}
			allRSs := []*appsv1.ReplicaSet{oldRS, newRS}
			rollout := newRollout("foo", test.rolloutReplicas, nil, newSelector, "", "")
			fake := fake.Clientset{}
			k8sfake := k8sfake.Clientset{}
			controller := &Controller{
				rolloutsclientset: &fake,
				kubeclientset:     &k8sfake,
				recorder:          &record.FakeRecorder{},
			}
			scaled, err := controller.reconcileOldReplicaSets(allRSs, oldRSs, newRS, rollout)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if !test.scaleExpected && scaled {
				t.Errorf("unexpected scaling: %v", k8sfake.Actions())
			}
			if test.scaleExpected && !scaled {
				t.Errorf("expected scaling to occur")
				return
			}
		})
	}
}
