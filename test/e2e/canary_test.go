// +build e2e

package e2e

import (
	"log"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	corev1 "k8s.io/api/core/v1"

	"github.com/argoproj/argo-rollouts/test/fixtures"
)

type CanarySuite struct {
	fixtures.E2ESuite
}

func TestCanarySuite(t *testing.T) {
	suite.Run(t, new(CanarySuite))
}

func (s *CanarySuite) SetupSuite() {
	s.E2ESuite.SetupSuite()
	s.ApplyManifests("@functional/analysistemplate-sleep-job.yaml")
}

func (s *CanarySuite) TestCanarySetCanaryScale() {
	s.T().Parallel()
	canarySteps := `
- pause: {}
- setCanaryScale:
    weight: 25
- pause: {}
- setWeight: 50
- pause: {}
- setCanaryScale:
    replicas: 3
- pause: {}
- setCanaryScale:
    matchTrafficWeight: true
- pause: {}
`
	s.Given().
		RolloutTemplate("@functional/nginx-template.yaml", "set-canary-scale").
		SetSteps(canarySteps).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		UpdateSpec().
		// at step 0
		WaitForRolloutStatus("Paused").
		Then().
		ExpectCanaryStablePodCount(0, 4).
		When().
		PromoteRollout().
		WaitForRolloutCanaryStepIndex(2).
		Then().
		// at step 2
		ExpectCanaryStablePodCount(1, 4).
		When().
		PromoteRollout().
		WaitForRolloutCanaryStepIndex(4).
		Then().
		// at step 4
		ExpectCanaryStablePodCount(1, 4).
		When().
		PromoteRollout().
		WaitForRolloutCanaryStepIndex(6).
		Then().
		// at step 6
		ExpectCanaryStablePodCount(3, 4).
		When().
		PromoteRollout().
		WaitForRolloutCanaryStepIndex(8).
		Then().
		// at step 8
		ExpectCanaryStablePodCount(2, 4).
		When().
		PromoteRollout().
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectCanaryStablePodCount(4, 4)
}

// TestRolloutScalingWhenPaused verifies behavior when scaling a rollout up/down when paused
func (s *FunctionalSuite) TestRolloutScalingWhenPaused() {
	s.Given().
		RolloutObjects(`@functional/rollout-basic.yaml`).
		SetSteps(`
- setWeight: 25
- pause: {}`).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		UpdateSpec().
		WaitForRolloutStatus("Paused").
		Then().
		ExpectCanaryStablePodCount(1, 1).
		When().
		ScaleRollout(8).
		WaitForRolloutAvailableReplicas(8).
		Then().
		ExpectCanaryStablePodCount(2, 6).
		When().
		ScaleRollout(4).
		WaitForRolloutAvailableReplicas(4).
		Then().
		ExpectCanaryStablePodCount(1, 3)
}

// TestRolloutScalingDuringUpdate verifies behavior when scaling a rollout up/down in middle of update
func (s *CanarySuite) TestRolloutScalingDuringUpdate() {
	s.Given().
		HealthyRollout(`
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: updatescaling
spec:
  replicas: 4
  strategy:
    canary:
      maxSurge: 2
      maxUnavailable: 0
  selector:
    matchLabels:
      app: updatescaling
  template:
    metadata:
      labels:
        app: updatescaling
    spec:
      containers:
      - name: updatescaling
        image: nginx:1.19-alpine
        resources:
          requests:
            memory: 16Mi
            cpu: 1m`).
		When().
		PatchSpec(`
spec:
  template:
    spec:
      containers:
      - name: updatescaling
        command: [/bad-command]`).
		WaitForRolloutReplicas(6).
		Then().
		ExpectCanaryStablePodCount(2, 4).
		When().
		ScaleRollout(8).
		WaitForRolloutReplicas(10).
		Then().
		// NOTE: the numbers below may change in the future.
		// See: https://github.com/argoproj/argo-rollouts/issues/738
		ExpectCanaryStablePodCount(6, 4).
		When().
		ScaleRollout(4)
	// WaitForRolloutReplicas(4) // this doesn't work yet (bug)
}

// TestReduceWeightAndHonorMaxUnavailable verifies we honor maxUnavailable when decreasing weight or aborting
func (s *CanarySuite) TestReduceWeightAndHonorMaxUnavailable() {
	s.Given().
		HealthyRollout(`
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: reduceweight-maxunavailable
spec:
  replicas: 3
  strategy:
    canary:
      maxSurge: 2
      maxUnavailable: 0
      steps:
      - setWeight: 100
      - pause: {}
      - setWeight: 0
      - pause: {}
      - setWeight: 100
      - analysis:
          templates:
          - templateName: sleep-job
          args:
          - name: exit-code
            value: "1"
  selector:
    matchLabels:
      app: reduceweight-maxunavailable
  template:
    metadata:
      labels:
        app: reduceweight-maxunavailable
    spec:
      containers:
      - name: reduceweight-maxunavailable
        image: nginx:1.19-alpine
        # slow down the start/stop of pods so our pod count checks will not flake
        lifecycle:
          postStart:
            exec:
              command: [sleep, "5"]
          preStop:
            exec:
              command: [sleep, "5"]
        resources:
          requests:
            memory: 16Mi
            cpu: 1m`).
		When().
		UpdateSpec().
		WaitForRolloutStatus("Paused").
		Then().
		ExpectCanaryStablePodCount(3, 0).
		When().
		PromoteRollout().
		Sleep(2*time.Second).
		Then().
		// verify we don't scale down immediately after and honor maxSurge/maxUnavailable
		ExpectCanaryStablePodCount(3, 2).
		When().
		WaitForRolloutCanaryStepIndex(3).
		PromoteRollout().
		WaitForRolloutStatus("Degraded").
		Sleep(2*time.Second).
		Then().
		ExpectAnalysisRunCount(1).
		// verify we don't scale down immediately after and honor maxSurge/maxUnavailable
		ExpectCanaryStablePodCount(3, 2)
}

// TestEphemeralMetadata tests the ephemeral metadata feature
func (s *CanarySuite) TestEphemeralMetadata() {
	podsHaveStableMetadata := func(pods *corev1.PodList) bool {
		for _, pod := range pods.Items {
			log.Printf("+%v", pod.Labels)
			if pod.Labels["role"] != "stable" {
				return false
			}
		}
		return true
	}
	podsHaveCanaryMetadata := func(pods *corev1.PodList) bool {
		for _, pod := range pods.Items {
			if pod.Labels["role"] != "canary" {
				return false
			}
		}
		return true
	}
	podsHaveCanaryMetadata2 := func(pods *corev1.PodList) bool {
		for _, pod := range pods.Items {
			if _, ok := pod.Labels["role"]; ok {
				return false
			}
			if pod.Labels["role2"] != "canary2" {
				return false
			}
		}
		return true
	}
	podsHaveStableMetadata2 := func(pods *corev1.PodList) bool {
		for _, pod := range pods.Items {
			if _, ok := pod.Labels["role"]; ok {
				return false
			}
			if pod.Labels["role2"] != "stable2" {
				return false
			}
		}
		return true
	}

	s.Given().
		RolloutObjects(`
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: ephemeral-metadata-canary
spec:
  replicas: 2
  strategy:
    canary:
      canaryMetadata:
        labels:
          role: canary
      stableMetadata:
        labels:
          role: stable
      steps:
      - setWeight: 50
      - pause: {}
  selector:
    matchLabels:
      app: ephemeral-metadata-canary
  template:
    metadata:
      labels:
        app: ephemeral-metadata-canary
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
		ExpectPods("all pods have stable metadata", podsHaveStableMetadata).
		When().
		WaitForRolloutStatus("Healthy").
		UpdateSpec().
		WaitForRolloutStatus("Paused").
		Then().
		ExpectRevisionPods("revision 2 has canary metadata", "2", podsHaveCanaryMetadata).
		ExpectRevisionPods("revision 1 has stable metadata", "1", podsHaveStableMetadata).
		When().
		// update canary metadata to different value, verify old data is gone, and replaced with new
		PatchSpec(`
spec:
  strategy:
    canary:
      canaryMetadata:
        labels:
          role: null
          role2: canary2`).
		Sleep(time.Second).
		Then().
		ExpectRevisionPods("revision 2 has canary metadata2", "2", podsHaveCanaryMetadata2).
		When().
		PromoteRollout().
		WaitForRolloutStatus("Healthy").
		Then().
		// after fully promoted, revision 2 should switch to stable metadata
		ExpectRevisionPods("revision 2 has stable metadata", "2", podsHaveStableMetadata).
		When().
		// if we change stable metadata, pods and replicasets should see the change
		PatchSpec(`
spec:
  strategy:
    canary:
      stableMetadata:
        labels:
          role: null
          role2: stable2`).
		Sleep(time.Second).
		Then().
		ExpectRevisionPods("revision 2 has stable metadata2", "2", podsHaveStableMetadata2)
}
