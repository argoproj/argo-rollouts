// +build e2e

package e2e

import (
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	corev1 "k8s.io/api/core/v1"

	"github.com/argoproj/argo-rollouts/test/fixtures"
)

type BlueGreenSuite struct {
	fixtures.E2ESuite
}

func TestBlueGreenSuite(t *testing.T) {
	suite.Run(t, new(BlueGreenSuite))
}

func (s *BlueGreenSuite) SetupSuite() {
	s.E2ESuite.SetupSuite()
}

// TestEphemeralMetadata tests the ephemeral metadata feature
func (s *BlueGreenSuite) TestEphemeralMetadata() {
	podsHaveActiveMetadata := func(pods *corev1.PodList) bool {
		for _, pod := range pods.Items {
			if pod.Labels["role"] != "active" {
				return false
			}
		}
		return true
	}
	podsHavePreviewMetadata := func(pods *corev1.PodList) bool {
		for _, pod := range pods.Items {
			if pod.Labels["role"] != "preview" {
				return false
			}
		}
		return true
	}
	podsHavePreviewMetadata2 := func(pods *corev1.PodList) bool {
		for _, pod := range pods.Items {
			if _, ok := pod.Labels["role"]; ok {
				return false
			}
			if pod.Labels["role2"] != "preview2" {
				return false
			}
		}
		return true
	}
	podsHaveActiveMetadata2 := func(pods *corev1.PodList) bool {
		for _, pod := range pods.Items {
			if _, ok := pod.Labels["role"]; ok {
				return false
			}
			if pod.Labels["role2"] != "active2" {
				return false
			}
		}
		return true
	}

	s.Given().
		RolloutObjects(`
apiVersion: v1
kind: Service
metadata:
  name: ephemeral-metadata
spec:
  ports:
  - port: 80
    targetPort: http
    protocol: TCP
    name: http
  selector:
    app: ephemeral-metadata
---
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: ephemeral-metadata
spec:
  replicas: 2
  strategy:
    blueGreen:
      previewMetadata:
        labels:
          role: preview
      activeMetadata:
        labels:
          role: active
      activeService: ephemeral-metadata
      autoPromotionEnabled: false
  selector:
    matchLabels:
      app: ephemeral-metadata
  template:
    metadata:
      labels:
        app: ephemeral-metadata
    spec:
      containers:
      - name: ephemeral-metadata
        image: nginx:1.19-alpine
        resources:
          requests:
            memory: 16Mi
            cpu: 1m
`).
		When().
		ApplyManifests().
		WaitForRolloutReplicas(2).
		Then().
		ExpectPods("all pods have active metadata", podsHaveActiveMetadata).
		When().
		WaitForRolloutStatus("Healthy").
		UpdateSpec().
		WaitForRolloutStatus("Paused").
		Then().
		ExpectRevisionPods("revision 2 has preview metadata", "2", podsHavePreviewMetadata).
		ExpectRevisionPods("revision 1 has active metadata", "1", podsHaveActiveMetadata).
		When().
		// update preview metadata to different value, verify old data is gone, and replaced with new
		PatchSpec(`
spec:
  strategy:
    blueGreen:
      previewMetadata:
        labels:
          role: null
          role2: preview2`).
		Sleep(time.Second).
		Then().
		ExpectRevisionPods("revision 2 has preview metadata2", "2", podsHavePreviewMetadata2).
		When().
		PromoteRollout().
		WaitForRolloutStatus("Healthy").
		Sleep(time.Second).
		Then().
		// after fully promoted, revision 2 should switch to active metadata
		ExpectRevisionPods("revision 2 has active metadata", "2", podsHaveActiveMetadata).
		When().
		// if we change active metadata, pods and replicasets should see the change
		PatchSpec(`
spec:
  strategy:
    blueGreen:
      activeMetadata:
        labels:
          role: null
          role2: active2`).
		Sleep(2*time.Second).
		Then().
		ExpectRevisionPods("revision 2 has active metadata2", "2", podsHaveActiveMetadata2)
}
