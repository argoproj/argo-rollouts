// +build e2e

package e2e

import (
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	appsv1 "k8s.io/api/apps/v1"

	"github.com/argoproj/argo-rollouts/test/fixtures"
)

type FunctionalSuite struct {
	fixtures.E2ESuite
}

func TestFunctionalSuite(t *testing.T) {
	suite.Run(t, new(FunctionalSuite))
}

func countReplicaSets(count int) fixtures.ReplicaSetExpectation {
	return func(rsets *appsv1.ReplicaSetList) bool {
		return len(rsets.Items) == count
	}
}

func (s *FunctionalSuite) TestRolloutAbortRetryPromote() {
	s.Given().
		HealthyRollout(`@functional/rollout-basic.yaml`).
		When().
		UpdateSpec().
		WaitForRolloutStatus("Paused").
		Then().
		ExpectReplicaSets("two replicasets", countReplicaSets(2)).
		When().
		AbortRollout().
		WaitForRolloutStatus("Degraded").
		RetryRollout().
		WaitForRolloutStatus("Paused").
		PromoteRollout().
		WaitForRolloutStatus("Healthy")
}

func (s *FunctionalSuite) TestRolloutRestart() {
	s.Given().
		HealthyRollout(`@functional/rollout-basic.yaml`).
		When().
		Sleep(time.Second). // need to sleep so that clock will advanced past pod creationTimestamp
		RestartRollout().
		WaitForRolloutStatus("Progressing").
		WaitForRolloutStatus("Healthy")
}

func (s *FunctionalSuite) TestMalformedRollout() {
	s.Given().
		HealthyRollout(`@expectedfailures/malformed-rollout.yaml`)
}

// TestContainerResourceFormats verifies resource requests are accepted in multiple formats and not
// rejected by validation
func (s *FunctionalSuite) TestContainerResourceFormats() {
	s.Given().
		HealthyRollout(`
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: container-resource-formats
spec:
  replicas: 0
  selector:
    matchLabels:
      app: container-resource-formats
  template:
    metadata:
      labels:
        app: container-resource-formats
    spec:
      containers:
      - name: a
        image: nginx:1.19-alpine
        command: [sleep, infinity]
        resources:
          requests:
            cpu: 0.001
      - name: b
        image: nginx:1.19-alpine
        command: [sleep, infinity]
        resources:
          requests:
            cpu: '0.001'
      - name: c
        image: nginx:1.19-alpine
        command: [sleep, infinity]
        resources:
          requests:
            cpu: 1m
  strategy:
    canary: {}
`)
}

// TestRolloutGoodToBadUpdate updates a healthy rollout to a bad image and verifies it honors maxSurge, maxUnavailable
// It then updates it to a second bad image, and verifies it balances between revision 1 and revision 3.
func (s *FunctionalSuite) TestRolloutGoodToBadUpdate() {
	s.Given().
		HealthyRollout(`
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: good2bad
spec:
  replicas: 4
  strategy:
    canary:
      maxSurge: 1
      maxUnavailable: 0
  selector:
    matchLabels:
      app: good2bad
  template:
    metadata:
      labels:
        app: good2bad
    spec:
      containers:
      - name: good2bad
        image: nginx:1.19-alpine
        resources:
          requests:
            memory: 16Mi
            cpu: 1m`).
		When().
		PatchSpec(`
spec:
  progressDeadlineSeconds: 10
  template:
    spec:
      containers:
      - name: good2bad
        command: [/bad-command]`). // update to a bad image
		WaitForRolloutStatus("Degraded").
		Then().
		ExpectCanaryStablePodCount(1, 4).
		When().
		PatchSpec(`
spec:
  template:
    spec:
      containers:
      - name: good2bad
        command: [/bad-command2]`). // update to another bad image
		WaitForRolloutStatus("Progressing").
		WaitForRolloutStatus("Degraded").
		Then().
		ExpectRevisionPodCount("1", 4).
		ExpectRevisionPodCount("2", 0).
		ExpectRevisionPodCount("3", 1)
}

// TestRolloutBadToGoodUpdate deploys a rollout which crashes, then updates it to a working image
// and verifies it is able to eventually make progress
func (s *FunctionalSuite) TestRolloutBadToGoodUpdate() {
	s.Given().
		RolloutObjects(`
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: bad2good
spec:
  replicas: 4
  progressDeadlineSeconds: 10
  strategy:
    canary:
      maxSurge: 1
      maxUnavailable: 0
  selector:
    matchLabels:
      app: bad2good
  template:
    metadata:
      labels:
        app: bad2good
    spec:
      containers:
      - name: bad2good
        image: nginx:1.19-alpine
        command: [/bad-command]
        resources:
          requests:
            memory: 16Mi
            cpu: 1m`).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Degraded").
		PatchSpec(`
spec:
  progressDeadlineSeconds: 60
  template:
    spec:
      containers:
      - name: bad2good
        command: null`).
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectRevisionPodCount("2", 4).
		ExpectRevisionPodCount("1", 0)
}

// TestRolloutBadToBadUpdate deploys a rollout which crashes, then updates it to another bad
// image that crashes, and verifies it still honors maxSurge/maxUnavailable.
func (s *FunctionalSuite) TestRolloutBadToBadUpdate() {
	s.Given().
		RolloutObjects(`
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: bad2bad
spec:
  replicas: 4
  progressDeadlineSeconds: 10
  strategy:
    canary:
      maxSurge: 1
      maxUnavailable: 0
  selector:
    matchLabels:
      app: bad2bad
  template:
    metadata:
      labels:
        app: bad2bad
    spec:
      containers:
      - name: bad2bad
        image: nginx:1.19-alpine
        command: [/bad-command]
        resources:
          requests:
            memory: 16Mi
            cpu: 1m`).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Degraded").
		PatchSpec(`
spec:
  template:
    spec:
      containers:
      - name: bad2bad
        command: [/bad-command2]`).
		WaitForRolloutStatus("Progressing").
		WaitForRolloutStatus("Degraded").
		Then().
		ExpectRevisionPodCount("2", 1).
		ExpectRevisionPodCount("1", 4)
}

// TestRolloutBadToGoodSetWeightUpdate deploys a rollout which crashes, then updates it to a working
// image with a canary weight of 50%
func (s *FunctionalSuite) TestRolloutBadToGoodSetWeightUpdate() {
	s.Given().
		RolloutObjects(`
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: bad2good-setweight
spec:
  replicas: 4
  progressDeadlineSeconds: 10
  strategy:
    canary:
      maxSurge: 1
      maxUnavailable: 0
      steps:
      - setWeight: 50
      - pause: {}
  selector:
    matchLabels:
      app: bad2good-setweight
  template:
    metadata:
      labels:
        app: bad2good-setweight
    spec:
      containers:
      - name: bad2good-setweight
        image: nginx:1.19-alpine
        command: [/bad-command]
        resources:
          requests:
            memory: 16Mi
            cpu: 1m`).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Degraded").
		PatchSpec(`
spec:
  template:
    spec:
      containers:
      - name: bad2good-setweight
        command: null`).
		WaitForRolloutStatus("Progressing").
		WaitForRolloutStatus("Degraded").
		Then().
		ExpectCanaryStablePodCount(2, 2)
}

// TestBlueGreenUpdate
func (s *FunctionalSuite) TestBlueGreenUpdate() {
	s.Given().
		HealthyRollout("@functional/rollout-bluegreen.yaml").
		When().
		Then().
		ExpectReplicaCounts(3, 3, 3, 3, 3). // desired, current, updated, ready, available
		When().
		UpdateSpec().
		WaitForRolloutStatus("Progressing").
		WaitForActiveRevision("2").
		Then().
		ExpectReplicaCounts(3, 6, 3, 3, 3).
		When().
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectReplicaCounts(3, 6, 3, 3, 3)
}

// TestBlueGreenPreviewReplicaCount verifies the previewReplicaCount feature
func (s *FunctionalSuite) TestBlueGreenPreviewReplicaCount() {
	s.Given().
		RolloutObjects(newService("bluegreen-preview-replicas-active")).
		RolloutObjects(newService("bluegreen-preview-replicas-preview")).
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

// TestBlueGreenToCanary tests behavior when migrating from bluegreen to canary
func (s *FunctionalSuite) TestBlueGreenToCanary() {
	s.Given().
		RolloutObjects(newService("bluegreen-to-canary")).
		HealthyRollout(`
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: bluegreen-to-canary
spec:
  replicas: 2
  strategy:
    blueGreen:
      activeService: bluegreen-to-canary
      scaleDownDelaySeconds: 5
  selector:
    matchLabels:
      app: bluegreen-to-canary
  template:
    metadata:
      labels:
        app: bluegreen-to-canary
    spec:
      containers:
      - name: bluegreen-to-canary
        image: nginx:1.19-alpine
        resources:
          requests:
            memory: 16Mi
            cpu: 1m
`).
		When().
		UpdateSpec(`
spec:
  template:
    metadata:
      annotations:
        foo: bar
  strategy:
    blueGreen: null
    canary:
      steps:
      - setWeight: 50
      - pause: {}
`).
		WaitForRolloutStatus("Paused").
		Then().
		ExpectReplicaCounts(2, 2, 1, 2, 2). // desired, current, updated, ready, available
		ExpectServiceSelector("bluegreen-to-canary", map[string]string{"app": "bluegreen-to-canary"})
}
