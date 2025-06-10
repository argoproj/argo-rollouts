package rollout

import (
	"context"
	"fmt"
	"strconv"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	clientset "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned"
	"github.com/argoproj/argo-rollouts/utils/labels"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	patchtypes "k8s.io/apimachinery/pkg/types"
)

type CurrentAnalysisRun interface {
	CurrentStatus() *v1alpha1.RolloutAnalysisRunStatus
	ShouldCancel(cancelOptions ...CancelOption) bool
	ShouldReturnCur(options ...ShouldReturnCurOption) bool
	NeedsNew(controllerPause bool, pauseConditions []v1alpha1.PauseCondition, abortedAt *metav1.Time) bool
	Infix(options ...InfixOption) string
	ARType() string
	AnalysisRun() *v1alpha1.AnalysisRun
	RolloutAnalysis() *v1alpha1.RolloutAnalysis
	Labels(podHash, instanceID string, options ...LabelsOption) map[string]string
	IsPresent() bool
	UpdateRun(run *v1alpha1.AnalysisRun)
}

type AnalysisRunEvent struct {
	msg         string
	EventType   string
	EventReason string
}

type cancelOpts struct {
	step               *v1alpha1.CanaryStep
	stepIndex          *int32
	backgroundAnalysis *v1alpha1.RolloutAnalysisBackground
	shouldSkip         bool
}

type CancelOption func(*cancelOpts)

func WithShouldSkip(shouldSkip bool) CancelOption {
	return func(opts *cancelOpts) {
		opts.shouldSkip = shouldSkip
	}
}

func WithBackgroundAnalysis(canaryStrat *v1alpha1.CanaryStrategy) CancelOption {
	var analysis *v1alpha1.RolloutAnalysisBackground
	if canaryStrat != nil {
		analysis = canaryStrat.Analysis
	}
	return func(opts *cancelOpts) {
		opts.backgroundAnalysis = analysis
	}
}

func WithStep(step *v1alpha1.CanaryStep) CancelOption {
	return func(opts *cancelOpts) {
		opts.step = step
	}
}
func WithStepIndex(index *int32) CancelOption {
	return func(opts *cancelOpts) {
		opts.stepIndex = index
	}
}

type shouldReturnCurOpts struct {
	pauseConditions []v1alpha1.PauseCondition
	abort           bool
}

type ShouldReturnCurOption func(*shouldReturnCurOpts)

func WithAbort(abort bool) ShouldReturnCurOption {
	return func(opts *shouldReturnCurOpts) {
		opts.abort = abort
	}
}
func WithConditions(conditions []v1alpha1.PauseCondition) ShouldReturnCurOption {
	return func(opts *shouldReturnCurOpts) {
		opts.pauseConditions = conditions
	}
}

type OptionalLabels struct {
	Labels []labels.Label[string, string]
}

type LabelsOption func(*OptionalLabels)

func WithStepIndexLabel(index *int32) LabelsOption {
	if index == nil {
		return func(options *OptionalLabels) {}
	}
	return func(options *OptionalLabels) {
		options.Labels = append(
			options.Labels,
			labels.NewLabel(
				v1alpha1.RolloutCanaryStepIndexLabel,
				strconv.Itoa(int(*index)),
			),
		)
	}
}

type InfixOpts struct {
	index *int32
}

type InfixOption func(*InfixOpts)

func InfixWithIndex(index *int32) InfixOption {
	return func(opts *InfixOpts) {
		opts.index = index
	}
}

type CurrentAnalysisRuns struct {
	CurrentBlueGreenPrePromotion  BlueGreenPrePromotionAR
	CurrentBlueGreenPostPromotion BlueGreenPostPromotionAR
	CurrentCanaryStep             CanaryStepAR
	CurrentCanaryBackground       CanaryBackgroundAR
}

type AnalysisContext struct {
	CurrentAnalysisRuns
	otherArs []*v1alpha1.AnalysisRun
	log      *log.Entry
}

func (ac *AnalysisContext) UpdateCurrentAnalysisRuns(ar *v1alpha1.AnalysisRun, artype string) *AnalysisContext {
	switch artype {
	case v1alpha1.RolloutTypePrePromotionLabel:
		ac.CurrentAnalysisRuns.CurrentBlueGreenPrePromotion = BlueGreenPrePromotionAR{
			BaseRun: BaseRun{
				Run: ar,
			},
		}
	case v1alpha1.RolloutTypePostPromotionLabel:
		ac.CurrentAnalysisRuns.CurrentBlueGreenPostPromotion = BlueGreenPostPromotionAR{
			BaseRun: BaseRun{
				Run: ar,
			},
		}
	case v1alpha1.RolloutTypeStepLabel:
		ac.CurrentAnalysisRuns.CurrentCanaryStep = CanaryStepAR{
			BaseRun: BaseRun{
				Run: ar,
			},
		}
	case v1alpha1.RolloutTypeBackgroundRunLabel:
		ac.CurrentAnalysisRuns.CurrentCanaryBackground = CanaryBackgroundAR{
			BaseRun: BaseRun{
				Run: ar,
			},
		}
	}

	return ac
}

func NewAnalysisContext(analysisRuns []*v1alpha1.AnalysisRun, r *v1alpha1.Rollout) *AnalysisContext {
	ac := &AnalysisContext{
		CurrentAnalysisRuns: CurrentAnalysisRuns{
			CurrentBlueGreenPrePromotion: BlueGreenPrePromotionAR{
				BaseRun: BaseRun{},
			},
			CurrentBlueGreenPostPromotion: BlueGreenPostPromotionAR{
				BaseRun: BaseRun{},
			},
			CurrentCanaryStep: CanaryStepAR{
				BaseRun: BaseRun{},
			},
			CurrentCanaryBackground: CanaryBackgroundAR{
				BaseRun: BaseRun{},
			},
		},
		otherArs: []*v1alpha1.AnalysisRun{},
		log:      nil,
	}
	otherArs := []*v1alpha1.AnalysisRun{}
	getArName := func(s *v1alpha1.RolloutAnalysisRunStatus) string {
		if s == nil {
			return ""
		}
		return s.Name
	}
	for i := range analysisRuns {
		ar := analysisRuns[i]
		if ar != nil {
			switch ar.Name {
			case getArName(r.Status.Canary.CurrentStepAnalysisRunStatus):
				ac.UpdateCurrentAnalysisRuns(ar, v1alpha1.RolloutTypeStepLabel)
			case getArName(r.Status.Canary.CurrentBackgroundAnalysisRunStatus):
				ac.UpdateCurrentAnalysisRuns(ar, v1alpha1.RolloutTypeBackgroundRunLabel)
			case getArName(r.Status.BlueGreen.PrePromotionAnalysisRunStatus):
				ac.UpdateCurrentAnalysisRuns(ar, v1alpha1.RolloutTypePrePromotionLabel)
			case getArName(r.Status.BlueGreen.PostPromotionAnalysisRunStatus):
				ac.UpdateCurrentAnalysisRuns(ar, v1alpha1.RolloutTypePostPromotionLabel)
			default:
				otherArs = append(otherArs, ar)
			}
		}
	}
	ac.otherArs = otherArs
	return ac
}

func (c *AnalysisContext) AllCurrentAnalysisRuns() []CurrentAnalysisRun {
	return []CurrentAnalysisRun{
		&c.CurrentBlueGreenPrePromotion,
		&c.CurrentBlueGreenPostPromotion,
		&c.CurrentCanaryStep,
		&c.CurrentCanaryBackground,
	}
}

func (c *AnalysisContext) CurrentAnalysisRunsToArray() []*v1alpha1.AnalysisRun {
	currentAnalysisRuns := []*v1alpha1.AnalysisRun{}
	if c.CurrentBlueGreenPrePromotion.Run != nil {
		currentAnalysisRuns = append(currentAnalysisRuns, c.CurrentBlueGreenPrePromotion.Run)
	}
	if c.CurrentBlueGreenPostPromotion.Run != nil {
		currentAnalysisRuns = append(currentAnalysisRuns, c.CurrentBlueGreenPostPromotion.Run)
	}
	if c.CurrentCanaryStep.Run != nil {
		currentAnalysisRuns = append(currentAnalysisRuns, c.CurrentCanaryStep.Run)
	}
	if c.CurrentCanaryBackground.Run != nil {
		currentAnalysisRuns = append(currentAnalysisRuns, c.CurrentCanaryBackground.Run)
	}
	return currentAnalysisRuns
}

func (c *AnalysisContext) AllAnalysisRuns() []*v1alpha1.AnalysisRun {
	return append(c.CurrentAnalysisRunsToArray(), c.otherArs...)
}

func (ac *AnalysisContext) BlueGreenPrePromotionAR() *v1alpha1.AnalysisRun {
	return ac.CurrentBlueGreenPrePromotion.AnalysisRun()
}

func (ac *AnalysisContext) BlueGreenPostPromotionAR() *v1alpha1.AnalysisRun {
	return ac.CurrentBlueGreenPostPromotion.AnalysisRun()
}

func (ac *AnalysisContext) CanaryStepAR() *v1alpha1.AnalysisRun {
	return ac.CurrentCanaryStep.AnalysisRun()
}

func (ac *AnalysisContext) CanaryBackgroundAR() *v1alpha1.AnalysisRun {
	return ac.CurrentCanaryBackground.AnalysisRun()
}
func (ac *AnalysisContext) BlueGreenPrePromotionARStatus() *v1alpha1.RolloutAnalysisRunStatus {
	return ac.CurrentBlueGreenPrePromotion.CurrentStatus()
}

func (ac *AnalysisContext) BlueGreenPostPromotionARStatus() *v1alpha1.RolloutAnalysisRunStatus {
	return ac.CurrentBlueGreenPostPromotion.CurrentStatus()
}

func (ac *AnalysisContext) CanaryStepARStatus() *v1alpha1.RolloutAnalysisRunStatus {
	return ac.CurrentCanaryStep.CurrentStatus()
}

func (ac *AnalysisContext) CanaryBackgroundARStatus() *v1alpha1.RolloutAnalysisRunStatus {
	return ac.CurrentCanaryBackground.CurrentStatus()
}

func (c *AnalysisContext) cancelAnalysisRuns(client clientset.Interface, analysisRuns []*v1alpha1.AnalysisRun) error {
	ctx := context.TODO()
	for _, ar := range analysisRuns {
		isNotCompleted := ar == nil || !ar.Status.Phase.Completed()
		if !ar.Spec.Terminate && isNotCompleted {
			c.log.WithField(logutil.AnalysisRunKey, ar.Name).Infof("Canceling the analysis run '%s'", ar.Name)
			_, err := client.ArgoprojV1alpha1().AnalysisRuns(ar.Namespace).Patch(ctx, ar.Name, patchtypes.MergePatchType, []byte(cancelAnalysisRun), metav1.PatchOptions{})
			if err != nil {
				if k8serrors.IsNotFound(err) {
					c.log.Warnf("AnalysisRun '%s' not found", ar.Name)
					continue
				}
				return err
			}
		}
	}
	return nil
}

func (c *AnalysisContext) deleteAnalysisRuns(client clientset.Interface, ars []*v1alpha1.AnalysisRun) error {
	ctx := context.TODO()
	for i := range ars {
		ar := ars[i]
		if ar.DeletionTimestamp != nil {
			continue
		}
		c.log.Infof("Trying to cleanup analysis run '%s'", ar.Name)
		err := client.ArgoprojV1alpha1().AnalysisRuns(ar.Namespace).Delete(ctx, ar.Name, metav1.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

func (c *AnalysisContext) emitAnalysisRunStatusChanges(prevStatus *v1alpha1.RolloutAnalysisRunStatus, ar *v1alpha1.AnalysisRun, arType string) *AnalysisRunEvent {
	if ar.Status.Phase != "" {
		if prevStatus == nil || prevStatus.Name == ar.Name && prevStatus.Status != ar.Status.Phase {
			prevStatusStr := "NoPreviousStatus"
			if prevStatus != nil {
				prevStatusStr = string(prevStatus.Status)
			}

			eventType := corev1.EventTypeNormal
			if ar.Status.Phase == v1alpha1.AnalysisPhaseFailed || ar.Status.Phase == v1alpha1.AnalysisPhaseError {
				eventType = corev1.EventTypeWarning
			}
			msg := fmt.Sprintf("%s Analysis Run '%s' Status New: '%s' Previous: '%s'", arType, ar.Name, ar.Status.Phase, prevStatusStr)
			return &AnalysisRunEvent{
				msg:         msg,
				EventType:   eventType,
				EventReason: "AnalysisRun" + string(ar.Status.Phase),
			}
		}
	}
	return nil
}

// reconcileAnalysisRunStatusChanges for each analysisRun type, the controller checks if the analysis run status has changed
// for that type
func (c *AnalysisContext) reconcileAnalysisRunStatusChanges(previousStatuses map[string]*v1alpha1.RolloutAnalysisRunStatus) []*AnalysisRunEvent {
	events := make([]*AnalysisRunEvent, 0)
	for _, run := range c.AllCurrentAnalysisRuns() {
		if run.IsPresent() {
			event := c.emitAnalysisRunStatusChanges(previousStatuses[run.ARType()], run.AnalysisRun(), run.ARType())
			if event != nil {
				events = append(events, event)
			}
		}
	}
	return events
}
