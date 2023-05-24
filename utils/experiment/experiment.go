package experiment

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	patchtypes "k8s.io/apimachinery/pkg/types"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	rolloutsclient "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/typed/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	"github.com/argoproj/argo-rollouts/utils/hash"
	timeutil "github.com/argoproj/argo-rollouts/utils/time"
)

var terminateExperimentPatch = []byte(`{"spec":{"terminate":true}}`)

func HasFinished(experiment *v1alpha1.Experiment) bool {
	return experiment.Status.Phase.Completed()
}

func Terminate(experimentIf rolloutsclient.ExperimentInterface, name string) error {
	ctx := context.TODO()
	_, err := experimentIf.Patch(ctx, name, patchtypes.MergePatchType, terminateExperimentPatch, metav1.PatchOptions{})
	return err
}

// IsTerminating returns whether or not an experiment is terminating, such as its analysis failed,
// or explicit termination.
func IsTerminating(experiment *v1alpha1.Experiment) bool {
	if experiment.Spec.Terminate {
		return true
	}
	if HasFinished(experiment) {
		return true
	}
	for _, ts := range experiment.Status.TemplateStatuses {
		switch ts.Status {
		case v1alpha1.TemplateStatusFailed, v1alpha1.TemplateStatusError:
			return true
		}
	}
	for _, run := range experiment.Status.AnalysisRuns {
		switch run.Phase {
		case v1alpha1.AnalysisPhaseFailed, v1alpha1.AnalysisPhaseError, v1alpha1.AnalysisPhaseInconclusive:
			return true
		}
	}
	return RequiredAnalysisRunsSuccessful(experiment, &experiment.Status)
}

func HasRequiredAnalysisRuns(ex *v1alpha1.Experiment) bool {
	for _, analysis := range ex.Spec.Analyses {
		if analysis.RequiredForCompletion {
			return true
		}
	}
	return false
}

// RequiredAnalysisRunsSuccessful has at least one required for completion analysis run
// and it completed successfully
func RequiredAnalysisRunsSuccessful(ex *v1alpha1.Experiment, exStatus *v1alpha1.ExperimentStatus) bool {
	if exStatus == nil {
		return false
	}
	hasRequiredAnalysisRun := false
	completedAllRequiredRuns := true
	for _, analysis := range ex.Spec.Analyses {
		if analysis.RequiredForCompletion {
			hasRequiredAnalysisRun = true
			analysisStatus := GetAnalysisRunStatus(*exStatus, analysis.Name)
			if analysisStatus == nil || analysisStatus.Phase != v1alpha1.AnalysisPhaseSuccessful {
				completedAllRequiredRuns = false
			}
		}
	}
	return hasRequiredAnalysisRun && completedAllRequiredRuns
}

// PassedDurations indicates if the experiment has run longer than the duration
func PassedDurations(experiment *v1alpha1.Experiment) (bool, time.Duration) {
	if experiment.Spec.Duration == "" {
		return false, 0
	}
	if experiment.Status.AvailableAt == nil {
		return false, 0
	}
	now := timeutil.MetaNow()
	dur, err := experiment.Spec.Duration.Duration()
	if err != nil {
		return false, 0
	}
	expiredTime := experiment.Status.AvailableAt.Add(dur)
	return now.After(expiredTime), expiredTime.Sub(now.Time)
}

func CalculateTemplateReplicasCount(experiment *v1alpha1.Experiment, template v1alpha1.TemplateSpec) int32 {
	if HasFinished(experiment) || IsTerminating(experiment) {
		return int32(0)
	}
	templateStatus := GetTemplateStatus(experiment.Status, template.Name)
	if templateStatus != nil && templateStatus.Status.Completed() {
		return int32(0)
	}
	return defaults.GetReplicasOrDefault(template.Replicas)
}

// GetTemplateStatusMapping returns a mapping of name to template statuses
func GetTemplateStatusMapping(status v1alpha1.ExperimentStatus) map[string]v1alpha1.TemplateStatus {
	mapping := make(map[string]v1alpha1.TemplateStatus, len(status.TemplateStatuses))
	for i := range status.TemplateStatuses {
		template := status.TemplateStatuses[i]
		mapping[template.Name] = template
	}
	return mapping
}

func GetCollisionCountForTemplate(experiment *v1alpha1.Experiment, template v1alpha1.TemplateSpec) *int32 {
	templateStatuses := GetTemplateStatusMapping(experiment.Status)
	templateStatus := templateStatuses[template.Name]
	var collisionCount *int32
	if templateStatus.CollisionCount != nil {
		collisionCount = templateStatus.CollisionCount
	}
	return collisionCount
}

// ReplicasetNameFromExperiment gets the replicaset name based off of the experiment and the template
func ReplicasetNameFromExperiment(experiment *v1alpha1.Experiment, template v1alpha1.TemplateSpec) string {
	// todo: review this method for deletion as it's not using
	collisionCount := GetCollisionCountForTemplate(experiment, template)
	podTemplateSpecHash := hash.ComputePodTemplateHash(&template.Template, collisionCount)
	return fmt.Sprintf("%s-%s-%s", experiment.Name, template.Name, podTemplateSpecHash)
}

// ExperimentByCreationTimestamp sorts a list of experiment by creation timestamp (earliest to latest), using their name as a tie breaker.
type ExperimentByCreationTimestamp []*v1alpha1.Experiment

func (o ExperimentByCreationTimestamp) Len() int      { return len(o) }
func (o ExperimentByCreationTimestamp) Swap(i, j int) { o[i], o[j] = o[j], o[i] }
func (o ExperimentByCreationTimestamp) Less(i, j int) bool {
	if o[i].CreationTimestamp.Equal(&o[j].CreationTimestamp) {
		return o[i].Name < o[j].Name
	}
	return o[i].CreationTimestamp.Before(&o[j].CreationTimestamp)
}

// GetTemplateStatus returns a TemplateStatus by name
func GetTemplateStatus(status v1alpha1.ExperimentStatus, name string) *v1alpha1.TemplateStatus {
	for _, ts := range status.TemplateStatuses {
		if ts.Name == name {
			return ts.DeepCopy()
		}
	}
	return nil
}

// SetTemplateStatus updates the experiment's template status with the new template status
func SetTemplateStatus(status *v1alpha1.ExperimentStatus, templateStatus v1alpha1.TemplateStatus) {
	for i, ts := range status.TemplateStatuses {
		if ts.Name == templateStatus.Name {
			status.TemplateStatuses[i] = templateStatus
			return
		}
	}
	status.TemplateStatuses = append(status.TemplateStatuses, templateStatus)
}

// GetAnalysisRunStatus gets an analysis run status by name
func GetAnalysisRunStatus(exStatus v1alpha1.ExperimentStatus, name string) *v1alpha1.ExperimentAnalysisRunStatus {
	for _, runStatus := range exStatus.AnalysisRuns {
		if runStatus.Name == name {
			return &runStatus
		}
	}
	return nil
}

// SetAnalysisRunStatus updates the experiment's analysis run status with the new analysis run status
func SetAnalysisRunStatus(exStatus *v1alpha1.ExperimentStatus, newRunStatus v1alpha1.ExperimentAnalysisRunStatus) {
	for i, runStatus := range exStatus.AnalysisRuns {
		if runStatus.Name == newRunStatus.Name {
			exStatus.AnalysisRuns[i] = newRunStatus
			return
		}
	}
	exStatus.AnalysisRuns = append(exStatus.AnalysisRuns, newRunStatus)
}

// templateStatusOrder is a list of template statuses sorted in best to worst condition
var templateStatusOrder = []v1alpha1.TemplateStatusCode{
	v1alpha1.TemplateStatusSuccessful,
	v1alpha1.TemplateStatusRunning,
	v1alpha1.TemplateStatusProgressing,
	v1alpha1.TemplateStatusError,
	v1alpha1.TemplateStatusFailed,
}

// TemplateIsWorse returns whether the new template status is a worser condition than the current.
func TemplateIsWorse(current, new v1alpha1.TemplateStatusCode) bool {
	currentIndex := 0
	newIndex := 0
	for i, code := range templateStatusOrder {
		if current == code {
			currentIndex = i
		}
		if new == code {
			newIndex = i
		}
	}
	return newIndex > currentIndex
}

// Worst returns the worst of the supplied status codes
func Worst(left, right v1alpha1.TemplateStatusCode) v1alpha1.TemplateStatusCode {
	if TemplateIsWorse(left, right) {
		return right
	}
	return left
}

// IsSemanticallyEqual checks to see if two experiments are semantically equal
func IsSemanticallyEqual(left, right v1alpha1.ExperimentSpec) bool {
	// we set these to both to false so that it does not factor into equality check
	left.Terminate = false
	right.Terminate = false
	leftBytes, err := json.Marshal(left)
	if err != nil {
		panic(err)
	}
	rightBytes, err := json.Marshal(right)
	if err != nil {
		panic(err)
	}
	return string(leftBytes) == string(rightBytes)
}
