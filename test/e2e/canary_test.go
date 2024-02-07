//go:build e2e
// +build e2e

package e2e

import (
	"log"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
	corev1 "k8s.io/api/core/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	rov1 "github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
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
- pause: {duration: 100s}
- setWeight: 50
- pause: {duration: 10s}
- setCanaryScale:
    replicas: 3
- pause: {}
- setCanaryScale:
    matchTrafficWeight: true
- pause: {duration: 5s}
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
func (s *CanarySuite) TestRolloutScalingWhenPaused() {
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

// TestRolloutWithMaxSurgeScalingDuringUpdate verifies behavior when scaling a rollout up/down in middle of update and with maxSurge 100%
func (s *CanarySuite) TestRolloutWithMaxSurgeScalingDuringUpdate() {
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
      maxSurge: 100%
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
		WaitForRolloutReplicas(7).
		Then().
		ExpectCanaryStablePodCount(4, 3).
		When().
		ScaleRollout(8).
		WaitForRolloutReplicas(11).
		Then().
		ExpectCanaryStablePodCount(8, 3).
		When().
		ScaleRollout(4).
		WaitForRolloutReplicas(7).
		Then().
		ExpectCanaryStablePodCount(4, 3)
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
		ScaleRollout(4).
		WaitForRolloutReplicas(6).
		Then().
		ExpectCanaryStablePodCount(2, 4)
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

func (s *CanarySuite) TestCanaryProgressDeadlineExceededWithPause() {
	s.Given().
		RolloutObjects(`
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: rollout-canary-with-pause
spec:
  replicas: 3
  revisionHistoryLimit: 2
  progressDeadlineSeconds: 5
  selector:
    matchLabels:
      app: rollout-canary-with-pause
  template:
    metadata:
      labels:
        app: rollout-canary-with-pause
    spec:
      containers:
      - name: rollouts-demo
        image: nginx:1.19-alpine
        ports:
        - containerPort: 80
        readinessProbe:
          initialDelaySeconds: 10
          httpGet:
            path: /
            port: 80
          periodSeconds: 30
  strategy:
    canary:
      steps:
      - setWeight: 20
      - pause: {}
`).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Degraded").
		WaitForRolloutStatus("Healthy").
		WaitForRolloutReplicas(3).
		UpdateSpec().
		WaitForRolloutStatus("Degraded").
		WaitForRolloutStatus("Paused").
		Then().
		ExpectCanaryStablePodCount(1, 3).
		When().
		PromoteRollout().
		WaitForRolloutStatus("Degraded").
		WaitForRolloutStatus("Healthy")
}

// TestCanaryScaleDownDelay verifies canary uses a scaleDownDelay when traffic routing is used,
// and verifies the annotation is properly managed
func (s *CanarySuite) TestCanaryScaleDownDelay() {
	s.Given().
		HealthyRollout(`@functional/canary-scaledowndelay.yaml`).
		When().
		UpdateSpec(`
spec:
  template:
    metadata:
      annotations:
        rev: two`). // update to revision 2
		WaitForRolloutStatus("Healthy").
		Sleep(2 * time.Second). // sleep is necessary since scale down delay annotation happens in s subsequent reconciliation
		Then().
		Assert(func(t *fixtures.Then) {
			rs1 := t.GetReplicaSetByRevision("1")
			assert.Equal(s.T(), int32(1), *rs1.Spec.Replicas)
			assert.NotEmpty(s.T(), rs1.Annotations[v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey])
		}).
		When().
		UpdateSpec(`
spec:
  template:
    metadata:
      annotations:
        rev: three`). // update to revision 3
		WaitForRolloutStatus("Healthy").
		Then().
		Assert(func(t *fixtures.Then) {
			time.Sleep(time.Second)
			// rs1 should be scaled down now because of scaleDownRevisionLimit
			rs1 := t.GetReplicaSetByRevision("1")
			assert.Equal(s.T(), int32(0), *rs1.Spec.Replicas)
			assert.Empty(s.T(), rs1.Annotations[v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey])

			rs2 := t.GetReplicaSetByRevision("2")
			assert.Equal(s.T(), int32(1), *rs2.Spec.Replicas)
			assert.NotEmpty(s.T(), rs2.Annotations[v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey])
		}).
		When().
		UpdateSpec(`
spec:
  template:
    metadata:
      annotations:
        rev: two`). // go back to revision 2
		WaitForRolloutStatus("Healthy").
		Then().
		Assert(func(t *fixtures.Then) {
			time.Sleep(time.Second)
			rs1 := t.GetReplicaSetByRevision("1")
			assert.Equal(s.T(), int32(0), *rs1.Spec.Replicas)
			assert.Empty(s.T(), rs1.Annotations[v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey])

			rs4 := t.GetReplicaSetByRevision("4")
			assert.Equal(s.T(), int32(1), *rs4.Spec.Replicas)
			assert.Empty(s.T(), rs4.Annotations[v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey])

			rs3 := t.GetReplicaSetByRevision("3")
			assert.Equal(s.T(), int32(1), *rs3.Spec.Replicas)
			assert.NotEmpty(s.T(), rs3.Annotations[v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey])
		})
}

// TestCanaryScaleDownOnAbort verifies scaledownOnAbort feature for canary
func (s *CanarySuite) TestCanaryScaleDownOnAbort() {
	s.Given().
		HealthyRollout(`@functional/canary-scaledownonabort.yaml`).
		When().
		UpdateSpec(). // update to revision 2
		WaitForRolloutStatus("Paused").
		AbortRollout().
		WaitForRolloutStatus("Degraded").
		Then().
		// Expect that the canary service selector has been moved back to stable
		ExpectServiceSelector("canary-scaledowndelay-canary", map[string]string{"app": "canary-scaledowndelay", "rollouts-pod-template-hash": "66597877b7"}, false).
		When().
		Sleep(3*time.Second).
		Then().
		ExpectRevisionPodCount("2", 0)
}

func (s *CanarySuite) TestCanaryScaleDownOnAbortNoTrafficRouting() {
	s.Given().
		HealthyRollout(`@functional/canary-scaledownonabortnotrafficrouting.yaml`).
		When().
		UpdateSpec(). // update to revision 2
		WaitForRolloutStatus("Paused").
		AbortRollout().
		WaitForRolloutStatus("Degraded").
		Then().
		// Expect that the canary service selector has been moved back to stable
		ExpectServiceSelector("canary-scaledowndelay-canary", map[string]string{"app": "canary-scaledowndelay", "rollouts-pod-template-hash": "66597877b7"}, false).
		When().
		Sleep(3*time.Second).
		Then().
		ExpectRevisionPodCount("2", 0)
}

func (s *CanarySuite) TestCanaryWithPausedRollout() {
	(s.Given().
		HealthyRollout(`@functional/rollout-canary-with-pause.yaml`).
		When().
		ApplyManifests().
		MarkPodsReady("1", 3). // mark all 3 pods ready
		WaitForRolloutStatus("Healthy").
		UpdateSpec(). // update to revision 2
		WaitForRolloutStatus("Paused").
		UpdateSpec(). // update to revision 3
		WaitForRolloutStatus("Paused").
		Then().
		ExpectRevisionPodCount("1", 3).
		ExpectRevisionPodCount("2", 0).
		ExpectRevisionPodCount("3", 1))
}

func (s *CanarySuite) TestCanaryUnScaleDownOnAbort() {
	s.Given().
		HealthyRollout(`@functional/canary-unscaledownonabort.yaml`).
		When().
		UpdateSpec(). // update to revision 2
		WaitForRolloutStatus("Paused").
		AbortRollout().
		WaitForRolloutStatus("Degraded").
		Sleep(3*time.Second).
		Then().
		ExpectRevisionPodCount("2", 1).
		ExpectRevisionScaleDown("2", false)
}

func (s *CanarySuite) TestCanaryDynamicStableScale() {
	s.Given().
		RolloutObjects(`@functional/canary-dynamic-stable-scale.yaml`).
		When().
		ApplyManifests().
		MarkPodsReady("1", 4). // mark all 4 pods ready
		WaitForRolloutStatus("Healthy").
		UpdateSpec().          // update to revision 2
		MarkPodsReady("2", 1). // mark 1 of 1 canary pods ready
		WaitForRolloutStatus("Paused").
		Sleep(2*time.Second).
		Then().
		ExpectRevisionPodCount("1", 3).
		ExpectRevisionPodCount("2", 1).
		When().
		PromoteRollout().
		MarkPodsReady("2", 2). // mark two more canary pods ready (3/3 canaries ready)
		WaitForRolloutCanaryStepIndex(3).
		Sleep(2*time.Second).
		Then().
		ExpectRevisionPodCount("1", 1).
		ExpectRevisionPodCount("2", 3).
		When().
		// Abort rollout and ensure we scale down the canary as stable scales up
		AbortRollout().
		MarkPodsReady("1", 2). // mark 2 stable pods as ready (3/4 stable are ready)
		WaitForRevisionPodCount("2", 1).
		Then().
		ExpectRevisionPodCount("1", 4).
		// Assert that the canary service selector is still not set to stable rs because of dynamic stable scale still in progress
		Assert(func(t *fixtures.Then) {
			canarySvc, stableSvc := t.GetServices()
			assert.NotEqual(s.T(), canarySvc.Spec.Selector["rollouts-pod-template-hash"], stableSvc.Spec.Selector["rollouts-pod-template-hash"])
		}).
		When().
		MarkPodsReady("1", 1). // mark last remaining stable pod as ready (4/4 stable are ready)
		WaitForRevisionPodCount("2", 0).
		Then().
		// Expect that the canary service selector is now set to stable because of dynamic stable scale is over and we have all pods up on stable rs
		ExpectServiceSelector("dynamic-stable-scale-canary", map[string]string{"app": "dynamic-stable-scale", "rollouts-pod-template-hash": "868d98995b"}, false).
		ExpectRevisionPodCount("1", 4)
}

// TestCanaryDynamicStableScaleRollbackToStable verifies when we rollback to stable with
// DynamicStableScale enabled, we do so in a safe manner without shifting traffic back to stable
// before it can handle it
func (s *CanarySuite) TestCanaryDynamicStableScaleRollbackToStable() {
	s.Given().
		RolloutObjects(`@functional/canary-dynamic-stable-scale.yaml`).
		When().
		ApplyManifests().
		MarkPodsReady("1", 4). // mark all 4 pods ready
		WaitForRolloutStatus("Healthy").
		UpdateSpec().
		MarkPodsReady("2", 1). // mark 1 of 1 canary pods ready
		WaitForRolloutStatus("Paused").
		Sleep(2*time.Second).
		Then().
		ExpectRevisionPodCount("1", 3).
		ExpectRevisionPodCount("2", 1).
		When().
		UndoRollout(1). // Rollback to stable (revision 1)
		Sleep(2*time.Second).
		Then().
		ExpectRevisionPodCount("3", 4). // Ensure we fully scale up the stable (now revision 3)
		ExpectRevisionPodCount("2", 1). // And do not scale down the previous desired (revision 2)
		Assert(func(t *fixtures.Then) {
			// Make sure canary service is still pointing to the previous desired (revision 2)
			rs3 := t.GetReplicaSetByRevision("3")
			rs2 := t.GetReplicaSetByRevision("2")
			canarySvc, stableSvc := t.GetServices()
			assert.Equal(s.T(), rs2.Labels[rov1.DefaultRolloutUniqueLabelKey], canarySvc.Spec.Selector["rollouts-pod-template-hash"])
			assert.Equal(s.T(), rs3.Labels[rov1.DefaultRolloutUniqueLabelKey], stableSvc.Spec.Selector["rollouts-pod-template-hash"])

			// Ensure we did not touch the weights even though we are "fully promoted"
			ro := t.GetRollout()
			assert.Equal(s.T(), rs2.Labels[rov1.DefaultRolloutUniqueLabelKey], ro.Status.Canary.Weights.Canary.PodTemplateHash)
			assert.Equal(s.T(), int32(25), ro.Status.Canary.Weights.Canary.Weight)
			assert.Equal(s.T(), rs3.Labels[rov1.DefaultRolloutUniqueLabelKey], ro.Status.Canary.Weights.Stable.PodTemplateHash)
			assert.Equal(s.T(), int32(75), ro.Status.Canary.Weights.Stable.Weight)
		}).
		When().
		MarkPodsReady("3", 1).           // marks the 4th pod of stableRS/newRS (revision 3) ready
		WaitForRevisionPodCount("2", 0). // make sure we scale down the previous desired (revision 2)
		Then().
		Assert(func(t *fixtures.Then) {
			// Make sure canary/stable service is updated to point to revision 3
			rs3 := t.GetReplicaSetByRevision("3")
			canarySvc, stableSvc := t.GetServices()
			assert.Equal(s.T(), rs3.Labels[rov1.DefaultRolloutUniqueLabelKey], canarySvc.Spec.Selector["rollouts-pod-template-hash"])
			assert.Equal(s.T(), rs3.Labels[rov1.DefaultRolloutUniqueLabelKey], stableSvc.Spec.Selector["rollouts-pod-template-hash"])

			// Ensure we are 100% back to stable
			ro := t.GetRollout()
			assert.Equal(s.T(), rs3.Labels[rov1.DefaultRolloutUniqueLabelKey], ro.Status.Canary.Weights.Canary.PodTemplateHash)
			assert.Equal(s.T(), int32(0), ro.Status.Canary.Weights.Canary.Weight)
			assert.Equal(s.T(), rs3.Labels[rov1.DefaultRolloutUniqueLabelKey], ro.Status.Canary.Weights.Stable.PodTemplateHash)
			assert.Equal(s.T(), int32(100), ro.Status.Canary.Weights.Stable.Weight)

		})
}
