//go:build e2e
// +build e2e

package e2e

import (
	"fmt"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

func (s *AnalysisSuite) TestCanaryStepAnalysisRetry() {
	s.testCanaryAnalysisRetry(false)
}

func (s *AnalysisSuite) TestCanaryBackgroundAnalysisRetry() {
	s.testCanaryAnalysisRetry(true)
}

func (s *AnalysisSuite) testCanaryAnalysisRetry(background bool) {
	template := func(exitCode string) string {
		return fmt.Sprintf(`
apiVersion: argoproj.io/v1alpha1
kind: AnalysisTemplate
metadata:
  name: canary-retry-job
spec:
  args:
  - name: exit-code
    value: "%s"
  metrics:
  - name: job
    count: 1
    provider:
      job:
        spec:
          backoffLimit: 0
          template:
            spec:
              restartPolicy: Never
              containers:
              - name: job
                image: nginx:1.19-alpine
                command: [sh, -c, "exit {{args.exit-code}}"]
`, exitCode)
	}
	strategy := `
      steps:
      - setWeight: 50
      - analysis:
          templates:
          - templateName: canary-retry-job
      - setWeight: 100`
	if background {
		strategy = `
      analysis:
        templates:
        - templateName: canary-retry-job
      steps:
      - setWeight: 50
      - pause: {}
      - setWeight: 100`
	}
	when := s.Given().
		RolloutObjects(template("1")).
		RolloutObjects(fmt.Sprintf(`
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: canary-retry
spec:
  replicas: 2
  strategy:
    canary:%s
  selector:
    matchLabels:
      app: canary-retry
  template:
    metadata:
      labels:
        app: canary-retry
    spec:
      containers:
      - name: canary-retry
        image: nginx:1.19-alpine
        readinessProbe:
          httpGet:
            path: /
            port: 80
          initialDelaySeconds: 5
          periodSeconds: 1
        resources:
          requests:
            memory: 16Mi
            cpu: 5m
`, strategy)).
		When().
		ApplyManifests().
		WaitForRolloutStatus("Healthy").
		UpdateSpec().
		WaitForRolloutStatus("Degraded").
		WaitForRevisionPodCount("2", 0).
		Then().
		ExpectStableRevision("1").
		ExpectAnalysisRunCount(1).
		When().
		ApplyManifests(template("0")).
		ScaleRollout(3).
		RetryRollout()
	if background {
		when.WaitForRolloutCondition(func(ro *v1alpha1.Rollout) bool {
			current := ro.Status.Canary.CurrentBackgroundAnalysisRunStatus
			return current != nil && current.Status == v1alpha1.AnalysisPhaseSuccessful
		}, "current background analysis succeeded").
			WaitForRolloutStatus("Paused").
			PromoteRollout()
	}
	when.WaitForRolloutStatus("Healthy").
		Then().
		ExpectStableRevision("2").
		ExpectRevisionPodCount("2", 3).
		ExpectAnalysisRunCount(2).
		ExpectAnalysisRuns("failed attempt retained and first retry succeeded", func(runs *v1alpha1.AnalysisRunList) bool {
			failed, successful := 0, 0
			for _, run := range runs.Items {
				switch run.Status.Phase {
				case v1alpha1.AnalysisPhaseFailed:
					failed++
				case v1alpha1.AnalysisPhaseSuccessful:
					successful++
				}
			}
			return failed == 1 && successful == 1
		})
}
