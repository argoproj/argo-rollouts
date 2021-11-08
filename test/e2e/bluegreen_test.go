//go:build e2e
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
  name: ephemeral-metadata-bg
spec:
  ports:
  - port: 80
    targetPort: http
    protocol: TCP
    name: http
  selector:
    app: ephemeral-metadata-bg
---
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: ephemeral-metadata-bg
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
      activeService: ephemeral-metadata-bg
      autoPromotionEnabled: false
  selector:
    matchLabels:
      app: ephemeral-metadata-bg
  template:
    metadata:
      labels:
        app: ephemeral-metadata-bg
    spec:
      containers:
      - name: ephemeral-metadata
        image: nginx:1.19-alpine
        resources:
          requests:
            memory: 16Mi
            cpu: 1m
      volumes:
      - name: cache-volume
        emptyDir: {}
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

func (s *BlueGreenSuite) TestBlueGreenProgressDeadlineExceededWithPause() {
	s.Given().
		RolloutObjects(`
kind: Service
apiVersion: v1
metadata:
  name: rollout-bluegreen-with-pause
spec:
  selector:
    app: rollout-bluegreen-with-pause
  ports:
  - protocol: TCP
    port: 80
    targetPort: 8080
---
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: rollout-bluegreen-with-pause
spec:
  replicas: 1
  revisionHistoryLimit: 2
  progressDeadlineSeconds: 5 # note this is less than initialDelaySeconds
  selector:
    matchLabels:
      app: rollout-bluegreen-with-pause
  template:
    metadata:
      labels:
        app: rollout-bluegreen-with-pause
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
    blueGreen: 
      autoPromotionEnabled: false
      activeService: rollout-bluegreen-with-pause
`).
		When().
		ApplyManifests().
		WaitForRolloutReplicas(1).
		WaitForRolloutStatus("Degraded").
		WaitForRolloutStatus("Healthy").
		UpdateSpec().
		WaitForRolloutStatus("Degraded").
		WaitForRolloutStatus("Paused").
		PromoteRollout().
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectActiveRevision("2")
}

func (s *BlueGreenSuite) TestBlueGreenProgressDeadlineExceededWithoutPause() {
	s.Given().
		RolloutObjects(`
kind: Service
apiVersion: v1
metadata:
  name: rollout-bluegreen-without-pause
spec:
  selector:
    app: rollout-bluegreen-without-pause
  ports:
  - protocol: TCP
    port: 80
    targetPort: 8080
---
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: rollout-bluegreen-without-pause
spec:
  replicas: 1
  revisionHistoryLimit: 2
  progressDeadlineSeconds: 5 # note this is less than initialDelaySeconds
  selector:
    matchLabels:
      app: rollout-bluegreen-without-pause
  template:
    metadata:
      labels:
        app: rollout-bluegreen-without-pause
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
    blueGreen: 
      autoPromotionEnabled: true
      activeService: rollout-bluegreen-without-pause
`).
		When().
		ApplyManifests().
		WaitForRolloutReplicas(1).
		WaitForRolloutStatus("Degraded").
		WaitForRolloutStatus("Healthy").
		UpdateSpec().
		WaitForRolloutStatus("Degraded").
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectActiveRevision("2")
}

// TestBlueGreenPreviewReplicaCount verifies the previewReplicaCount feature
func (s *BlueGreenSuite) TestBlueGreenPreviewReplicaCount() {
	s.Given().
		RolloutObjects(newService("bluegreen-preview-replicas-active", "bluegreen-preview-replicas")).
		RolloutObjects(newService("bluegreen-preview-replicas-preview", "bluegreen-preview-replicas")).
		RolloutObjects(`
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: bluegreen-preview-replicas
spec:
  replicas: 2
  strategy:
    blueGreen:
      activeService: bluegreen-preview-replicas-active
      previewService: bluegreen-preview-replicas-preview
      previewReplicaCount: 1
      scaleDownDelaySeconds: 5
      autoPromotionEnabled: false
  selector:
    matchLabels:
      app: bluegreen-preview-replicas
  template:
    metadata:
      labels:
        app: bluegreen-preview-replicas
    spec:
      containers:
      - name: bluegreen-preview-replicas
        image: nginx:1.19-alpine
        resources:
          requests:
            memory: 16Mi
            cpu: 1m
`).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		UpdateSpec().
		WaitForRolloutStatus("Paused").
		Then().
		ExpectRevisionPodCount("2", 1).
		ExpectRevisionPodCount("1", 2).
		ExpectReplicaCounts(2, 3, 1, 2, 2). // desired, current, updated, ready, available
		When().
		PromoteRollout().
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectReplicaCounts(2, 4, 2, 2, 2)
}

// TestBlueGreenPreviewReplicaCountPromoteFull verifies promote full works with previewReplicaCount
func (s *FunctionalSuite) TestBlueGreenPreviewReplicaCountPromoteFull() {
	s.Given().
		RolloutObjects(newService("bluegreen-preview-replicas-active", "bluegreen-preview-replicas-promote-full")).
		RolloutObjects(`
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: bluegreen-preview-replicas-promote-full
spec:
  replicas: 2
  progressDeadlineSeconds: 1   # use a very short value to cause Degraded condition frequently
  strategy:
    blueGreen:
      activeService: bluegreen-preview-replicas-active
      previewReplicaCount: 1
      autoPromotionEnabled: false
  selector:
    matchLabels:
      app: bluegreen-preview-replicas-promote-full
  template:
    metadata:
      labels:
        app: bluegreen-preview-replicas-promote-full
    spec:
      containers:
      - name: bluegreen-preview-replicas-promote-full
        image: nginx:1.19-alpine
        resources:
          requests:
            memory: 16Mi
            cpu: 1m
`).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		UpdateSpec().
		WaitForRolloutStatus("Paused").
		Sleep(2*time.Second). // sleep for longer than progressDeadlineSeconds
		Then().
		ExpectRolloutStatus("Paused").      // the fact that we are paused for longer than progressDeadlineSeconds, should not cause Degraded
		ExpectReplicaCounts(2, 3, 1, 2, 2). // desired, current, updated, ready, available
		When().
		PromoteRolloutFull().
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectReplicaCounts(2, 4, 2, 2, 2)
}
