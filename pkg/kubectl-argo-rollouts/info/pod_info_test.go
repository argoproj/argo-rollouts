package info

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func basePod() *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "main"},
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:  "main",
					Ready: true,
					State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
				},
			},
		},
	}
}

func TestNewPodInfo_Unknown(t *testing.T) {
	pod := basePod()
	pod.Status.Phase = corev1.PodUnknown
	// Simulate the node lifecycle controller setting Status.Reason to "NodeLost"
	// and stale container state from before the node went unreachable.
	pod.Status.Reason = "NodeLost"
	pod.Status.ContainerStatuses[0].State = corev1.ContainerState{
		Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"},
	}

	info := newPodInfo(pod)

	assert.Equal(t, "Unknown", info.Status)
	assert.Equal(t, IconUnknown, info.Icon)
	// Stale container state should not affect ready count or restarts
	assert.Equal(t, "0/1", info.Ready)
	assert.Equal(t, int32(0), info.Restarts)
}

func TestNewPodInfo_Terminating(t *testing.T) {
	deletionTime := metav1.NewTime(time.Now())
	pod := basePod()
	pod.DeletionTimestamp = &deletionTime

	info := newPodInfo(pod)

	assert.Equal(t, "Terminating", info.Status)
	assert.Equal(t, IconProgressing, info.Icon)
	assert.Equal(t, "0/1", info.Ready)
	assert.Equal(t, int32(0), info.Restarts)
}

func TestNewPodInfo_TerminatingNotUnknown(t *testing.T) {
	// A pod that is both deleted and in Unknown phase (node lost while being deleted)
	// should show Unknown, not Terminating — the Unknown early-return fires first.
	deletionTime := metav1.NewTime(time.Now())
	pod := basePod()
	pod.DeletionTimestamp = &deletionTime
	pod.Status.Phase = corev1.PodUnknown

	info := newPodInfo(pod)

	assert.Equal(t, "Unknown", info.Status)
	assert.Equal(t, IconUnknown, info.Icon)
}
