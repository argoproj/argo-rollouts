// +build e2e

package e2e

import (
	"testing"

	"github.com/stretchr/testify/suite"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

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
		RestartRollout().
		WaitForRolloutStatus("Progressing").
		WaitForRolloutStatus("Healthy")
}

// TestContainerResourceFormats verifies resource requests are accepted in multiple formats and not
// rejected by validation
func (s *FunctionalSuite) TestContainerResourceFormats() {
	millicore, _ := resource.ParseQuantity("1m")
	podsHaveCorrectCPU := func(pods *corev1.PodList) bool {
		return pods.Items[0].Spec.Containers[0].Resources.Requests.Cpu().Equal(millicore)
	}
	s.Given().
		HealthyRollout(`
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: container-resource-formats
spec:
  replicas: 1
  selector:
    matchLabels:
      app: container-resource-formats
  template:
    metadata:
      labels:
        app: container-resource-formats
    spec:
      terminationGracePeriodSeconds: 1
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
`).When().Then().ExpectPods("pod cpu preserved", podsHaveCorrectCPU)
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
