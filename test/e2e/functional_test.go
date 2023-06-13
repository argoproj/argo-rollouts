//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/argoproj/argo-rollouts/controller"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/test/fixtures"
)

type FunctionalSuite struct {
	fixtures.E2ESuite
}

func TestFunctionalSuite(t *testing.T) {
	suite.Run(t, new(FunctionalSuite))
}

func (s *FunctionalSuite) SetupSuite() {
	s.E2ESuite.SetupSuite()
	// shared analysis templates for suite
	s.ApplyManifests("@functional/analysistemplate-sleep-job.yaml")
}

func (s *FunctionalSuite) TestRolloutAbortRetryPromote() {
	ctx, cancel := context.WithCancel(context.TODO())
	defer cancel()
	s.Given().
		StartEventWatch(ctx).
		HealthyRollout(`
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: abort-retry-promote
spec:
  strategy:
    canary: 
      steps:
      - setWeight: 50
      - pause: {duration: 3s}
  selector:
    matchLabels:
      app: abort-retry-promote
  template:
    metadata:
      labels:
        app: abort-retry-promote
    spec:
      containers:
      - name: abort-retry-promote
        image: nginx:1.19-alpine
        resources:
          requests:
            memory: 16Mi
            cpu: 1m
`).
		When().
		UpdateSpec().
		WaitForRolloutStatus("Paused").
		Then().
		ExpectRevisionPodCount("1", 1).
		ExpectRevisionPodCount("2", 1).
		When().
		AbortRollout().
		WaitForRolloutStatus("Degraded").
		RetryRollout().
		WaitForRolloutStatus("Paused").
		Then().
		ExpectRevisionPodCount("1", 1).
		ExpectRevisionPodCount("2", 1).
		When().
		WaitForRolloutStatus("Healthy"). // will auto-promote after `pause: {duration: 3s}` step
		Then().
		ExpectRevisionPodCount("1", 0).
		ExpectRevisionPodCount("2", 1).
		ExpectRolloutEvents([]string{
			"RolloutAddedToInformer", // Rollout added to informer cache
			"RolloutNotCompleted",    // Rollout not completed, started update to revision 0 (7fd9b5545c)
			"RolloutUpdated",         // Rollout updated to revision 1
			"NewReplicaSetCreated",   // Created ReplicaSet abort-retry-promote-698fbfb9dc (revision 1)
			"ScalingReplicaSet",      // Scaled up ReplicaSet abort-retry-promote-698fbfb9dc (revision 1) from 0 to 1
			"RolloutCompleted",       // Rollout completed update to revision 1 (698fbfb9dc): Initial deploy
			"RolloutNotCompleted",
			"RolloutUpdated",       // Rollout updated to revision 2
			"NewReplicaSetCreated", // Created ReplicaSet abort-retry-promote-75dcb5ddd6 (revision 2)
			"ScalingReplicaSet",    // Scaled up ReplicaSet abort-retry-promote-75dcb5ddd6 (revision 2) from 0 to 1
			"RolloutStepCompleted", // Rollout step 1/2 completed (setWeight: 50)
			"RolloutPaused",        // Rollout is paused (CanaryPauseStep)
			"ScalingReplicaSet",    // Scaled down ReplicaSet abort-retry-promote-75dcb5ddd6 (revision 2) from 1 to 0
			"RolloutAborted",       // Rollout aborted update to revision 2
			"ScalingReplicaSet",    // Scaled up ReplicaSet abort-retry-promote-75dcb5ddd6 (revision 2) from 0 to 1
			"RolloutStepCompleted", // Rollout step 1/2 completed (setWeight: 50)
			"RolloutPaused",        // Rollout is paused (CanaryPauseStep)
			"RolloutStepCompleted", // Rollout step 2/2 completed (pause: 3s)
			"RolloutResumed",       // Rollout is resumed
			"ScalingReplicaSet",    // Scaled down ReplicaSet abort-retry-promote-698fbfb9dc (revision 1) from 1 to 0
			"RolloutCompleted",     // Rollout completed update to revision 2 (75dcb5ddd6): Completed all 2 canary steps
		})
}

// TestCanaryPromoteFull verifies promotion of a canary under the following scenarios:
// 1. at a pause step with duration
// 2. in the middle of analysis
// 3. full promotion after abort
func (s *FunctionalSuite) TestCanaryPromoteFull() {
	s.Given().
		HealthyRollout(`
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: canary-promote-full
spec:
  strategy:
    canary:
      # this analysis should not run because it has a starting step of 4 which we never get to
      analysis:
        templates:
        - templateName: sleep-job
        startingStep: 4

      steps:
      - pause: {duration: 24h}
      - analysis:
          templates:
          - templateName: sleep-job
          args:
          - name: duration
            value: 24h
      - pause: {}
      - analysis: # we should never get to step 4
          templates:
          - templateName: sleep-job
  selector:
    matchLabels:
      app: canary-promote-full
  template:
    metadata:
      labels:
        app: canary-promote-full
    spec:
      containers:
      - name: canary-promote-full
        image: nginx:1.19-alpine
        resources:
          requests:
            memory: 16Mi
            cpu: 1m
`).
		When().
		UpdateSpec().
		WaitForRolloutStatus("Paused"). // At step 1 (pause: {duration: 24h})
		PromoteRollout().
		Sleep(2*time.Second).
		Then().
		ExpectRollout("status.currentStepIndex == 1", func(r *v1alpha1.Rollout) bool {
			return *r.Status.CurrentStepIndex == 1
		}).
		ExpectRolloutStatus("Progressing"). // At step 2 (analysis: sleep-job - 24h)
		ExpectAnalysisRunCount(1).
		When().
		PromoteRollout().
		Sleep(2 * time.Second).
		WaitForRolloutStatus("Paused"). // At step 3 (pause: {})
		AbortRollout().
		Then().
		When().
		Sleep(time.Second).
		PromoteRolloutFull().
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectAnalysisRunCount(1) // no new analysis was created
}

// TestBlueGreenPromoteFull verifies behavior when performing full promotion with a blue-green strategy
func (s *FunctionalSuite) TestBlueGreenPromoteFull() {
	s.Given().
		RolloutObjects(newService("bluegreen-promote-full-active", "bluegreen-promote-full")).
		RolloutObjects(`
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: bluegreen-promote-full
spec:
  replicas: 3
  strategy:
    blueGreen:
      activeService: bluegreen-promote-full-active
      autoPromotionEnabled: false
      prePromotionAnalysis:
        templates:
        - templateName: sleep-job
      postPromotionAnalysis:
        templates:
        - templateName: sleep-job
  selector:
    matchLabels:
      app: bluegreen-promote-full
  template:
    metadata:
      labels:
        app: bluegreen-promote-full
    spec:
      containers:
      - name: bluegreen-promote-full
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
		Sleep(time.Second).
		PromoteRolloutFull().
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectAnalysisRunCount(0)
}

func (s *FunctionalSuite) TestRolloutRestart() {
	s.Given().
		HealthyRollout(`
---
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: rollout-restart
spec:
  replicas: 4
  strategy:
    canary:
      maxUnavailable: 75%
      steps:
      - setWeight: 25
      - pause: {}
  selector:
    matchLabels:
      app: rollout-restart
  template:
    metadata:
      labels:
        app: rollout-restart
    spec:
      containers:
      - name: rollout-restart
        image: nginx:1.19-alpine
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
            cpu: 1m
`).
		When().
		UpdateSpec().
		WaitForRolloutStatus("Paused").
		Sleep(time.Second). // need to sleep so that clock will advanced past pod creationTimestamp
		RestartRollout().
		Sleep(2*time.Second).
		Then().
		ExpectReplicaCounts(4, 4, 1, 1, 1)
}

// Test which verifies pod restart honors PodDisruptionBudget
func (s *FunctionalSuite) TestRolloutPDBRestart() {
	s.Given().
		HealthyRollout(`
---
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: rollout-pdb-restart
spec:
  minAvailable: 4
  selector:
    matchLabels:
      app: rollout-pdb-restart
---
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: rollout-pdb-restart
spec:
  replicas: 4
  strategy:
    canary:
      maxUnavailable: 100%
      steps:
      - setWeight: 25
      - pause: {}
  selector:
    matchLabels:
      app: rollout-pdb-restart
  template:
    metadata:
      labels:
        app: rollout-pdb-restart
    spec:
      containers:
      - name: rollout-pdb-restart
        image: nginx:1.19-alpine
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
            cpu: 1m
`).
		When().
		UpdateSpec().
		WaitForRolloutStatus("Paused").
		Sleep(time.Second). // need to sleep so that clock will advanced past pod creationTimestamp
		RestartRollout().
		Sleep(2*time.Second).
		Then().
		ExpectReplicaCounts(4, 4, 1, 4, 4). // ensure PDB prevented rollout from deleting pod
		When().
		DeleteObject("pdb", "rollout-pdb-restart").
		WaitForRolloutAvailableReplicas(0) // wait for rollout to retry deletion (30s)
}

// Test which verifies an array named 'items' is deployable. Example test is in the 'volumes' spec
func (s *FunctionalSuite) TestRolloutPodVolumesItemsSpec() {
	s.Given().
		HealthyRollout(`
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: rollout-items
data:
  game.properties: |
    enemy.types=aliens,monsters
    player.maximum-lives=5
  user-interface.properties: |
    color.good=purple
    color.bad=yellow
    allow.textmode=true
---
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: rollout-items
spec:
  replicas: 2
  strategy:
    canary:
      maxUnavailable: 100%
      steps:
      - setWeight: 25
      - pause: {}
  selector:
    matchLabels:
      app: rollout-items
  template:
    metadata:
      labels:
        app: rollout-items
    spec:
      containers:
      - name: rollout-items
        image: nginx:1.19-alpine
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
            cpu: 1m
        volumeMounts:
        - name: rollout-items
          mountPath: "/config"
          readOnly: true
      volumes:
        - name: rollout-items
          configMap:
            name: rollout-items
            items:
            - key: "game.properties"
              path: "game.properties"
            - key: "user-interface.properties"
              path: "user-interface.properties"
`)
}

func (s *FunctionalSuite) TestMalformedRollout() {
	s.Given().
		HealthyRollout(`@expectedfailures/malformed-rollout.yaml`)
}

func (s *FunctionalSuite) TestMalformedRolloutEphemeralCtr() {
	s.Given().
		HealthyRollout(`@expectedfailures/malformed-rollout-ephemeral.yaml`)
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
      - pause:
          duration: 5s
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
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectRevisionPodCount("2", 4).
		ExpectRevisionPodCount("1", 0)
}

// TestBlueGreenUpdate
func (s *FunctionalSuite) TestBlueGreenUpdate() {
	ctx, cancel := context.WithCancel(context.TODO())
	defer cancel()
	s.Given().
		StartEventWatch(ctx).
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
		ExpectReplicaCounts(3, 6, 3, 3, 3).
		ExpectRolloutEvents([]string{
			"RolloutAddedToInformer", // Rollout added to informer cache
			"RolloutUpdated",         // Rollout updated to revision 1
			"NewReplicaSetCreated",   // Created ReplicaSet bluegreen-7dcd8f8869 (revision 1)
			"ScalingReplicaSet",      // Scaled up ReplicaSet bluegreen-7dcd8f8869 (revision 1) from 0 to 3
			"RolloutCompleted",       // Rollout completed update to revision 1 (7dcd8f8869): Initial deploy
			"SwitchService",          // Switched selector for service 'bluegreen' from '' to '7dcd8f8869'
			"RolloutUpdated",         // Rollout updated to revision 2
			"NewReplicaSetCreated",   // Created ReplicaSet bluegreen-5498785cd6 (revision 2)
			"RolloutNotCompleted",    // Rollout went to not completed state started update to revision 2 (85c6899)
			"ScalingReplicaSet",      // Scaled up ReplicaSet bluegreen-5498785cd6 (revision 2) from 0 to 3
			"SwitchService",          // Switched selector for service 'bluegreen' from '7dcd8f8869' to '6c779b88b6'
			"RolloutCompleted",       // Rollout completed update to revision 2 (6c779b88b6): Completed blue-green update
		})
}

// TestBlueGreenToCanary tests behavior when migrating from bluegreen to canary
func (s *FunctionalSuite) TestBlueGreenToCanary() {
	s.Given().
		RolloutObjects(newService("bluegreen-to-canary", "bluegreen-to-canary")).
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
		WaitForRolloutAvailableReplicas(2).
		Then().
		ExpectReplicaCounts(2, 2, 1, 2, 2). // desired, current, updated, ready, available
		ExpectServiceSelector("bluegreen-to-canary", map[string]string{"app": "bluegreen-to-canary"}, false)
}

// TestFixInvalidSpec verifies we recover from an InvalidSpec after applying
func (s *FunctionalSuite) TestFixInvalidSpec() {
	s.Given().
		RolloutObjects(`
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: fix-invalid-spec
spec:
  replicas: 0
  strategy:
    canary:
      analysis:
        templates:
        - templateName: doesnt-exist-yet
  selector:
    matchLabels:
      app: fix-invalid-spec
  template:
    metadata:
      labels:
        app: fix-invalid-spec
    spec:
      containers:
      - name: fix-invalid-spec
        image: nginx:1.19-alpine
        resources:
          requests:
            memory: 16Mi
            cpu: 1m
`).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Degraded").
		Then().
		Given().
		RolloutObjects(`
apiVersion: argoproj.io/v1alpha1
kind: AnalysisTemplate
metadata:
  name: doesnt-exist-yet
spec:
  metrics:
  - name: web
    interval: 5s
    successCondition: result.major == '1'
    provider:
      web:
        url: https://kubernetes.default.svc/version
        insecure: true
`).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy")
}

// TestBlueGreenScaleDownDelay verifies the scaleDownDelay feature
func (s *FunctionalSuite) TestBlueGreenScaleDownDelay() {
	s.Given().
		RolloutObjects(newService("bluegreen-scaledowndelay-active", "bluegreen-scaledowndelay")).
		RolloutObjects(`
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: bluegreen-scaledowndelay
spec:
  replicas: 1
  strategy:
    blueGreen:
      activeService: bluegreen-scaledowndelay-active
      scaleDownDelaySeconds: 86400 # one day
      scaleDownDelayRevisionLimit: 2
  selector:
    matchLabels:
      app: bluegreen-scaledowndelay
  template:
    metadata:
      labels:
        app: bluegreen-scaledowndelay
    spec:
      containers:
      - name: bluegreen-scaledowndelay
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
		WaitForRolloutStatus("Progressing").
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectRevisionPodCount("2", 1).
		ExpectRevisionPodCount("1", 1).
		ExpectReplicaCounts(1, 2, 1, 1, 1). // desired, current, updated, ready, available
		When().
		UpdateSpec().
		WaitForRolloutStatus("Progressing").
		WaitForRolloutStatus("Healthy").
		UpdateSpec().
		WaitForRolloutStatus("Progressing").
		WaitForRolloutStatus("Healthy").
		Sleep(time.Second).
		Then().
		ExpectRevisionPodCount("4", 1).
		ExpectRevisionPodCount("3", 1).
		ExpectRevisionPodCount("2", 1).
		ExpectRevisionPodCount("1", 0).
		ExpectReplicaCounts(1, 3, 1, 1, 1).
		When().
		// lower scaleDownDelayRevisionLimit to 1 old RS. it should cause revision 2 to ScaleDown
		PatchSpec(`
spec:
  strategy:
    blueGreen:
      scaleDownDelayRevisionLimit: 1`).
		Sleep(time.Second).
		Then().
		ExpectRevisionPodCount("4", 1).
		ExpectRevisionPodCount("3", 1).
		ExpectRevisionPodCount("2", 0).
		ExpectRevisionPodCount("1", 0).
		ExpectReplicaCounts(1, 2, 1, 1, 1)
}

// TestBlueGreenAbortExceedProgressDeadline verifies the AbortExceedProgressDeadline feature
func (s *FunctionalSuite) TestBlueGreenExceedProgressDeadlineAbort() {
	s.Given().
		RolloutObjects(newService("bluegreen-scaledowndelay-active", "bluegreen-scaledowndelay")).
		RolloutObjects(`
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: bluegreen-scaledowndelay
spec:
  replicas: 1
  strategy:
    blueGreen:
      activeService: bluegreen-scaledowndelay-active
      abortScaleDownDelaySeconds: 2
  selector:
    matchLabels:
      app: bluegreen-scaledowndelay
  template:
    metadata:
      labels:
        app: bluegreen-scaledowndelay
    spec:
      containers:
      - name: bluegreen-scaledowndelay
        image: nginx:1.19-alpine
        resources:
          requests:
            memory: 16Mi
            cpu: 1m
`).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		PatchSpec(`
spec:
  progressDeadlineAbort: false
  progressDeadlineSeconds: 1
  template:
    spec:
      containers:
      - name: bad2good
        image: nginx:1.19-alpine-argo-error
        command: null`).
		WaitForRolloutStatus("Degraded").
		Sleep(3*time.Second).
		Then().
		ExpectRevisionPodCount("2", 1).
		ExpectRollout("Abort=False", func(r *v1alpha1.Rollout) bool {
			return r.Status.Abort == false
		}).
		When().
		PatchSpec(`
spec:
  progressDeadlineAbort: true
  progressDeadlineSeconds: 1
  template:
    spec:
      containers:
      - name: bad2good
        image: nginx:1.19-alpine-argo-error
        command: null`).
		WaitForRolloutStatus("Degraded").
		Sleep(3*time.Second).
		Then().
		ExpectRevisionPodCount("2", 0).
		ExpectRollout("Abort=True", func(r *v1alpha1.Rollout) bool {
			return r.Status.Abort == true && len(r.Status.Conditions) == 4
		})
}

// TestBlueGreenScaleDownOnAbort verifies the scaleDownOnAbort feature
func (s *FunctionalSuite) TestBlueGreenScaleDownOnAbort() {
	s.Given().
		RolloutObjects(newService("bluegreen-preview-replicas-active", "bluegreen-preview-replicas")).
		RolloutObjects(newService("bluegreen-preview-replicas-preview", "bluegreen-preview-replicas")).
		RolloutObjects(`
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: bluegreen-scaledown-on-abort
spec:
  replicas: 2
  strategy:
    blueGreen:
      abortScaleDownDelaySeconds: 1
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
		When().
		AbortRollout().
		WaitForRolloutStatus("Degraded").
		Sleep(3*time.Second).
		Then().
		ExpectRevisionPodCount("2", 0)
}

func (s *FunctionalSuite) TestKubectlWaitForPaused() {
	s.Given().
		HealthyRollout(`
kind: Service
apiVersion: v1
metadata:
  name: kubectl-wait-paused
spec:
  selector:
    app: kubectl-wait-paused
  ports:
  - protocol: TCP
    port: 80
    targetPort: 80
---
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: kubectl-wait-paused
spec:
  replicas: 1
  selector:
    matchLabels:
      app: kubectl-wait-paused
  template:
    metadata:
      labels:
        app: kubectl-wait-paused
    spec:
      containers:
      - name: kubectl-wait-paused
        image: nginx:1.19-alpine
        imagePullPolicy: Always
        ports:
        - containerPort: 80
  strategy:
    blueGreen:
      activeService: kubectl-wait-paused
      autoPromotionEnabled: false
`).
		When().
		UpdateSpec().
		Then().
		ExpectRollout("Paused=True", func(r *v1alpha1.Rollout) bool {
			cmd := exec.Command("kubectl", "wait", "--for=condition=Paused", fmt.Sprintf("rollout/%s", r.Name))
			out, err := cmd.CombinedOutput()
			return err == nil && strings.Contains(string(out), fmt.Sprintf("rollout.argoproj.io/%s condition met", r.Name))
		}).
		ExpectRolloutStatus("Paused").
		ExpectActiveRevision("1").
		When().
		PromoteRollout().
		Then().
		ExpectRollout("Paused=False", func(r *v1alpha1.Rollout) bool {
			cmd := exec.Command("kubectl", "wait", "--for=condition=Paused=False", fmt.Sprintf("rollout/%s", r.Name))
			out, err := cmd.CombinedOutput()
			return err == nil && strings.Contains(string(out), fmt.Sprintf("rollout.argoproj.io/%s condition met", r.Name))
		}).
		ExpectRolloutStatus("Healthy").
		ExpectActiveRevision("2")
}

func (s *FunctionalSuite) TestCompleteRolloutRestart() {
	s.Given().
		HealthyRollout(`
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: rollout-restart
spec:
  progressDeadlineAbort: true
  progressDeadlineSeconds: 15
  replicas: 2
  selector:
    matchLabels:
      app: ollout-restart
  template:
    metadata:
      labels:
        app: ollout-restart
    spec:
      containers:
      - name: ollout-restart
        image: nginx:1.19-alpine
        imagePullPolicy: Always
  strategy:
    canary:
      steps:
      - setWeight: 20
`).
		When().
		WatchRolloutStatus("Healthy").
		Sleep(16 * time.Second). // give it enough time to pass the progressDeadlineSeconds
		Then().
		When().
		RestartRollout().
		WatchRolloutStatus("Healthy")
}

func (s *FunctionalSuite) TestKubectlWaitForCompleted() {
	s.Given().
		HealthyRollout(`
kind: Service
apiVersion: v1
metadata:
  name: kubectl-wait-healthy
spec:
  selector:
    app: kubectl-wait-healthy
  ports:
  - protocol: TCP
    port: 80
    targetPort: 80
---
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: kubectl-wait-healthy
spec:
  replicas: 1
  selector:
    matchLabels:
      app: kubectl-wait-healthy
  template:
    metadata:
      labels:
        app: kubectl-wait-healthy
    spec:
      containers:
      - name: kubectl-wait-healthy
        image: nginx:1.19-alpine
        imagePullPolicy: Always
        ports:
        - containerPort: 80
        readinessProbe:
          initialDelaySeconds: 5
          httpGet:
            path: /
            port: 80
          periodSeconds: 10

  strategy:
    blueGreen:
      activeService: kubectl-wait-healthy
      autoPromotionEnabled: true
`).
		When().
		UpdateSpec().
		Then().
		ExpectRollout("Healthy=False", func(r *v1alpha1.Rollout) bool {
			cmd := exec.Command("kubectl", "wait", "--for=condition=Healthy=False", fmt.Sprintf("rollout/%s", r.Name))
			out, err := cmd.CombinedOutput()
			return err == nil && strings.Contains(string(out), fmt.Sprintf("rollout.argoproj.io/%s condition met", r.Name))
		}).
		ExpectRolloutStatus("Progressing").
		ExpectActiveRevision("1").
		ExpectRollout("Healthy=True", func(r *v1alpha1.Rollout) bool {
			cmd := exec.Command("kubectl", "wait", "--for=condition=Healthy=True", fmt.Sprintf("rollout/%s", r.Name))
			out, err := cmd.CombinedOutput()
			return err == nil && strings.Contains(string(out), fmt.Sprintf("rollout.argoproj.io/%s condition met", r.Name))
		}).
		ExpectActiveRevision("2")
}

func (s *FunctionalSuite) TestWorkloadRefTemplate() {
	s.Given().
		RolloutObjects(`
---
apiVersion: apps/v1
kind: Deployment
metadata:
  labels:
    app.kubernetes.io/instance: rollout-canary
  name: rollout-ref-deployment
spec:
  replicas: 0
  selector:
    matchLabels:
      app: rollout-ref-deployment
  template:
    metadata:
      labels:
        app: rollout-ref-deployment
    spec:
      containers:
        - name: rollouts-demo
          image: argoproj/rollouts-demo:green
---
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: rollout-ref-deployment
spec:
  replicas: 1
  workloadRef:
    apiVersion: apps/v1
    kind: Deployment
    name: rollout-ref-deployment
  selector:
    matchLabels:
      app: rollout-demo-deploy
  template:
    metadata:
      labels:
        app: rollout-ref-deployment
  strategy:
    blueGreen: 
      activeService: rollout-bluegreen-active
`).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Degraded").
		Then().
		ExpectRollout("error due to workload ref and template", func(r *v1alpha1.Rollout) bool {
			return len(r.Status.Conditions) == 1 && strings.Contains(r.Status.Conditions[0].Message, "template must be empty for workload reference rollout")
		})
}

func (s *FunctionalSuite) TestWorkloadRef() {
	s.Given().
		RolloutObjects(`
kind: Service
apiVersion: v1
metadata:
  name: rollout-bluegreen-active
spec:
  selector:
    app: rollout-ref-deployment
  ports:
  - protocol: TCP
    port: 80
    targetPort: 8080
---
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: rollout-ref-deployment
spec:
  replicas: 1
  progressDeadlineSeconds: 5
  revisionHistoryLimit: 2
  workloadRef:
    apiVersion: apps/v1
    kind: Deployment
    name: rollout-ref-deployment
  strategy:
    blueGreen: 
      activeService: rollout-bluegreen-active
`).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Degraded").
		Then().
		// verify that existing service is not switched to degraded rollout pods
		ExpectServiceSelector("rollout-bluegreen-active", map[string]string{"app": "rollout-ref-deployment"}, false).
		When().
		ApplyManifests(`
apiVersion: apps/v1
kind: Deployment
metadata:
  labels:
    app.kubernetes.io/instance: rollout-canary
  name: rollout-ref-deployment
spec:
  replicas: 0
  selector:
    matchLabels:
      app: rollout-ref-deployment
  template:
    metadata:
      labels:
        app: rollout-ref-deployment
    spec:
      containers:
        - name: rollouts-demo
          image: argoproj/rollouts-demo:green
    `).
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectRollout("WorkloadObservedGeneration is 1", func(r *v1alpha1.Rollout) bool {
			if r.Status.WorkloadObservedGeneration != "1" {
				return false
			}
			return true
		}).
		// verify that service is switched after rollout is healthy
		ExpectServiceSelector("rollout-bluegreen-active", map[string]string{"app": "rollout-ref-deployment"}, true).
		When().
		UpdateResource(appsv1.SchemeGroupVersion.WithResource("deployments"), "rollout-ref-deployment", func(res *unstructured.Unstructured) error {
			containers, _, err := unstructured.NestedSlice(res.Object, "spec", "template", "spec", "containers")
			if err != nil {
				return err
			}
			containers[0] = map[string]interface{}{
				"name":  "rollouts-demo",
				"image": "argoproj/rollouts-demo:error",
			}
			return unstructured.SetNestedSlice(res.Object, containers, "spec", "template", "spec", "containers")
		}).
		WaitForRolloutStatus("Degraded").
		Then().
		ExpectRollout("WorkloadObservedGeneration is 2 after workload ref updated", func(r *v1alpha1.Rollout) bool {
			if r.Status.WorkloadObservedGeneration != "2" {
				return false
			}
			return true
		}).
		When().
		UpdateResource(appsv1.SchemeGroupVersion.WithResource("deployments"), "rollout-ref-deployment", func(res *unstructured.Unstructured) error {
			containers, _, err := unstructured.NestedSlice(res.Object, "spec", "template", "spec", "containers")
			if err != nil {
				return err
			}
			containers[0] = map[string]interface{}{
				"name":  "rollouts-demo",
				"image": "argoproj/rollouts-demo:blue",
			}
			return unstructured.SetNestedSlice(res.Object, containers, "spec", "template", "spec", "containers")
		}).
		WaitForRolloutStatus("Healthy").
		UpdateSpec(`
spec:
  workloadRef:
    apiVersion: apps/v1
    kind: Deployment
    name: non-existent-deploy
`).
		WaitForRolloutStatus("Degraded").
		UpdateSpec(`
spec:
  workloadRef:
    apiVersion: apps/v1
    kind: Deployment
    name: rollout-ref-deployment
`).
		WaitForRolloutStatus("Healthy").
		Then().
		// verify that service is switched after rollout is healthy
		ExpectServiceSelector("rollout-bluegreen-active", map[string]string{"app": "rollout-ref-deployment"}, true).
		ExpectRollout("Resolved template not persisted", func(rollout *v1alpha1.Rollout) bool {
			return rollout.Spec.Selector == nil && len(rollout.Spec.Template.Spec.Containers) == 0
		}).
		When().
		ApplyManifests(`
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: rollout-ref-deployment
spec:
  replicas: 1
  selector:
    matchLabels:
      app: rollout-ref-deployment
  progressDeadlineSeconds: 5
  revisionHistoryLimit: 2
  strategy:
    blueGreen:
      activeService: rollout-bluegreen-active
  template:
    metadata:
      labels:
        app: rollout-ref-deployment
    spec:
      containers:
        - name: rollouts-demo
          image: argoproj/rollouts-demo:blue
`).WaitForRolloutStatus("Healthy").
		Then().
		ExpectRollout("WorkloadObservedGeneration must be removed after switch to inline template", func(r *v1alpha1.Rollout) bool {
			if r.Status.WorkloadObservedGeneration != "" {
				return false
			}
			return true
		})
}

// TestControllerMetrics is a basic test to verify prometheus /metrics endpoint is functional
func (s *FunctionalSuite) TestControllerMetrics() {
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/metrics", controller.DefaultMetricsPort))
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), http.StatusOK, resp.StatusCode)
}

func (s *FunctionalSuite) TestRolloutPauseDurationGreaterThanProgressDeadlineSeconds() {
	(s.Given().
		HealthyRollout(`
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: rollout-canary
spec:
  replicas: 3
  progressDeadlineSeconds: 5
  selector:
    matchLabels:
      app: rollout-canary
  template:
    metadata:
      labels:
        app: rollout-canary
    spec:
      containers:
      - name: rollouts-demo
        image: nginx:1.19-alpine
        ports:
        - containerPort: 80
        resources:
          requests:
            memory: 16Mi
            cpu: 5m
  strategy:
    canary:
      steps:
      - setWeight: 32
      - pause: {duration: 30s}
      - setWeight: 67
`).
		When().
		UpdateSpec().
		WatchRolloutStatus("Healthy").
		Then().
		ExpectRolloutStatus("Healthy"))
}
