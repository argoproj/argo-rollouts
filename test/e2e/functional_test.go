// +build e2e

package e2e

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

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

// TestCanaryPromoteFull verifies behavior when performing full promotion with a canary strategy
func (s *FunctionalSuite) TestCanaryPromoteFull() {
	s.Given().
		HealthyRollout(`
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: canary-promote-full
spec:
  replicas: 3
  strategy:
    canary:
      maxUnavailable: 0
      analysis:
        templates:
        - templateName: sleep-job
        startingStep: 2
      steps:
      - pause: {}
      - pause: {}
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
		AbortRollout().
		Sleep(time.Second).
		PromoteRolloutFull().
		WaitForRolloutStatus("Healthy").
		Then().
		ExpectAnalysisRunCount(0)
}

// TestBlueGreenPromoteFull verifies behavior when performing full promotion with a blue-green strategy
func (s *FunctionalSuite) TestBlueGreenPromoteFull() {
	s.Given().
		RolloutObjects(newService("bluegreen-promote-full-active")).
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
apiVersion: policy/v1beta1
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
		RolloutObjects(newService("bluegreen-scaledowndelay-active")).
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

func (s *FunctionalSuite) TestKubectlWaitForPaused() {
	s.Given().
		RolloutObjects(`
kind: Service
apiVersion: v1
metadata:
  name: rollout-bluegreen-active
spec:
  selector:
    app: rollout-bluegreen
  ports:
  - protocol: TCP
    port: 80
    targetPort: 8080
---
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: rollout-bluegreen
spec:
  replicas: 1
  revisionHistoryLimit: 2
  selector:
    matchLabels:
      app: rollout-bluegreen
  template:
    metadata:
      labels:
        app: rollout-bluegreen
    spec:
      containers:
      - name: rollouts-demo
        image: argoproj/rollouts-demo:blue
        imagePullPolicy: Always
        ports:
        - containerPort: 8080
  strategy:
    blueGreen:
      activeService: rollout-bluegreen-active
      autoPromotionEnabled: false
`).
		When().
		ApplyManifests().
		WaitForRolloutReplicas(1).
		WaitForRolloutStatus("Healthy").
		UpdateSpec().
		Then().
		ExpectRollout("Paused", func(r *v1alpha1.Rollout) bool {
			cmd := exec.Command("kubectl", "wait", "--for=condition=Paused", fmt.Sprintf("rollout/%s", r.Name))
			out, err := cmd.CombinedOutput()
			return err == nil && strings.Contains(string(out), "rollout.argoproj.io/rollout-bluegreen condition met")
		}).
		When().
		PromoteRollout().
		Then().
		ExpectRollout("UnPaused", func(r *v1alpha1.Rollout) bool {
			cmd := exec.Command("kubectl", "wait", "--for=condition=Paused=False", fmt.Sprintf("rollout/%s", r.Name))
			return cmd.Run() == nil
		}).
		ExpectActiveRevision("2")
}

func (s *FunctionalSuite) TestKubectlWaitForCompleted() {
	s.Given().
		RolloutObjects(`
kind: Service
apiVersion: v1
metadata:
  name: rollout-bluegreen-active
spec:
  selector:
    app: rollout-bluegreen
  ports:
  - protocol: TCP
    port: 80
    targetPort: 8080
---
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: rollout-bluegreen
spec:
  replicas: 1
  revisionHistoryLimit: 2
  selector:
    matchLabels:
      app: rollout-bluegreen
  template:
    metadata:
      labels:
        app: rollout-bluegreen
    spec:
      containers:
      - name: rollouts-demo
        image: argoproj/rollouts-demo:blue
        imagePullPolicy: Always
        ports:
        - containerPort: 8080
  strategy:
    blueGreen:
      activeService: rollout-bluegreen-active
      autoPromotionEnabled: true
`).
		When().
		ApplyManifests().
		WaitForRolloutReplicas(1).
		WaitForRolloutStatus("Healthy").
		UpdateSpec().
		Then().
		ExpectRollout("Completed", func(r *v1alpha1.Rollout) bool {
			cmd := exec.Command("kubectl", "wait", "--for=condition=Completed=False", fmt.Sprintf("rollout/%s", r.Name))
			return cmd.Run() == nil
		}).
		When().
		PromoteRollout().
		Then().
		ExpectRollout("Completed", func(r *v1alpha1.Rollout) bool {
			cmd := exec.Command("kubectl", "wait", "--for=condition=Completed=True", fmt.Sprintf("rollout/%s", r.Name))
			return cmd.Run() == nil
		}).
		ExpectActiveRevision("2")
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
    app: rollout-bluegreen
  ports:
  - protocol: TCP
    port: 80
    targetPort: 8080
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
          image: argoproj/rollouts-demo:error
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
		ExpectServiceSelector("rollout-bluegreen-active", map[string]string{"app": "rollout-bluegreen"}, false).
		When().
		UpdateResource(appsv1.SchemeGroupVersion.WithResource("deployments"), "rollout-ref-deployment", func(res *unstructured.Unstructured) error {
			containers, _, err := unstructured.NestedSlice(res.Object, "spec", "template", "spec", "containers")
			if err != nil {
				return err
			}
			containers[0] = map[string]interface{}{
				"name":  "rollouts-demo",
				"image": "argoproj/rollouts-demo:green",
			}
			return unstructured.SetNestedSlice(res.Object, containers, "spec", "template", "spec", "containers")
		}).
		WaitForRolloutStatus("Healthy").
		Then().
		// verify that service is switched after rollout is healthy
		ExpectServiceSelector("rollout-bluegreen-active", map[string]string{"app": "rollout-bluegreen"}, true).
		ExpectRollout("Resolved template not persisted", func(rollout *v1alpha1.Rollout) bool {
			return rollout.Spec.Selector == nil && len(rollout.Spec.Template.Spec.Containers) == 0
		})
}
