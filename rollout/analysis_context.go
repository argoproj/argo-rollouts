package rollout

import (
	"context"
	"fmt"
	"strconv"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	clientset "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned"
	analysisutil "github.com/argoproj/argo-rollouts/utils/analysis"
	"github.com/argoproj/argo-rollouts/utils/labels"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
	log "github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	patchtypes "k8s.io/apimachinery/pkg/types"
)

const (
	cancelAnalysisRun = `{
		"spec": {
			"terminate": true
		}
	}`
)

type AnalysisRunEvent struct {
	msg         string
	EventType   string
	EventReason string
}

// The CurrentAnalysisRun interface represents a current analysis run in a rollout.
// It provides methods for accessing and manipulating the analysis run's state.
// BaseRun is a concrete implementation of CurrentAnalysisRun that represents a base analysis run and provides default
// behaviors for all CurrentAnalysisRun methods.
// Any individual CurrentAnalysisRun method can be implemented by a concrete implementation that wraps a BaseRun instance
// to provide custom behavior for specific methods while utilizing the default behavior for other methods.
type CurrentAnalysisRun interface {
	// CurrentStatus returns the current status of the analysis run.
	CurrentStatus() *v1alpha1.RolloutAnalysisRunStatus

	// AnalysisRun returns the analysis run object.
	RolloutAnalysis(options ...RolloutAnalysisOption) *v1alpha1.RolloutAnalysis

	// ShouldCancel determines whether the analysis run should be canceled based on the provided options.
	// cancelOptions: a variable number of CancelOption values that influence the cancellation decision.
	// Returns true if the analysis run should be canceled, false otherwise.
	// If no options are provided, the default behavior is to cancel the analysis run.
	ShouldCancel(cancelOptions ...CancelOption) bool

	// ShouldReturnCur determines whether the analysis run should return to a previous state based on the provided options.
	// options: a variable number of ShouldReturnCurOption values that influence the return decision.
	// Returns true if the analysis run should return to a previous state, false otherwise.
	// default behavior is to check if v1alpha1.PauseReasonInconclusiveAnalysis is in optional pause conditions list.
	ShouldReturnCur(options ...ShouldReturnCurOption) bool

	// NeedsNew determines whether a new analysis run is needed based on the controller pause state, pause conditions, and aborted time.
	// controllerPause: a boolean indicating whether the controller is paused.
	// pauseConditions: a list of v1alpha1.PauseCondition values that influence the pause decision.
	// abortedAt: a metav1.Time value indicating when the analysis run was aborted.
	// Returns true if a new analysis run is needed, false otherwise.
	NeedsNew(controllerPause bool, pauseConditions []v1alpha1.PauseCondition, abortedAt *metav1.Time) bool

	// Infix returns a string representation of the analysis run with optional infix options.
	// options: a variable number of InfixOption values that influence the infix string.
	// Returns the infix string representation of the analysis run.
	// default behavior is to return an empty string.
	Infix(options ...InfixOption) string

	// ARType returns the type of analysis run.
	// default behavior is to return an empty string.
	ARType() string

	// AnalysisRun returns the underlying analysis run object.
	AnalysisRun() *v1alpha1.AnalysisRun

	// Labels returns a map of labels for the analysis run.
	// podHash: a string representing the pod hash.
	// instanceID: a string representing the instance ID.
	// options: a variable number of LabelsOption values that influence the label map.
	// Any new optional Labels can be extended via new LabelsOption
	// Returns the label map for the analysis run.
	Labels(podHash, instanceID string, options ...LabelsOption) map[string]string

	// IsPresent checks whether the underlying analysis run is present.
	// default behavior is to evaluate the underlying analysis run pointer is not nil.
	IsPresent() bool

	// UpdateRun updates the underlying analysis run to point to the new run object.
	// run: the new analysis run object to update with.
	UpdateRun(run *v1alpha1.AnalysisRun)

	// OutsideAnalysisBoundaries determines whether the analysis run is outside the analysis boundaries.
	// options: a variable number of OutsideAnalysisBoundariesOption values that influence the decision.
	// default behavior is to return false.
	OutsideAnalysisBoundaries(options ...OutsideAnalysisBoundariesOption) bool

	// setPauseOrAbort sets the pause or abort context for the analysis run.
	// based on the current run status Phase.
	setPauseOrAbort(pauseCxt *pauseContext)
}

// ShouldCancel Optional params
type cancelOpts struct {
	step               *v1alpha1.CanaryStep
	stepIndex          *int32
	analysis           *v1alpha1.RolloutAnalysis
	backgroundAnalysis *v1alpha1.RolloutAnalysisBackground
	shouldSkip         bool
}

type CancelOption func(*cancelOpts)

func WithShouldSkip(shouldSkip bool) CancelOption {
	return func(opts *cancelOpts) {
		opts.shouldSkip = shouldSkip
	}
}

func WithBackgroundAnalysis(strategy *v1alpha1.RolloutStrategy) CancelOption {
	if strategy == nil || strategy.Canary == nil || strategy.Canary.Analysis == nil {
		return func(opts *cancelOpts) {}
	}
	return func(opts *cancelOpts) {
		opts.backgroundAnalysis = strategy.Canary.Analysis
	}
}

func WithAnalysis(analysis *v1alpha1.RolloutAnalysis) CancelOption {
	return func(opts *cancelOpts) {
		opts.analysis = analysis
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

// shouldReturnCur() Optional params
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

// Labels() Optional params
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

// RolloutAnalysis() Optional params
type RolloutAnalysisOpts struct {
	canary     *v1alpha1.CanaryStrategy
	canaryStep *v1alpha1.CanaryStep
	blueGreen  *v1alpha1.BlueGreenStrategy
}

type RolloutAnalysisOption func(*RolloutAnalysisOpts)

func WithCanary(canary *v1alpha1.CanaryStrategy) RolloutAnalysisOption {
	return func(opts *RolloutAnalysisOpts) {
		opts.canary = canary
	}
}

func WithCanaryStep(canaryStep *v1alpha1.CanaryStep) RolloutAnalysisOption {
	return func(opts *RolloutAnalysisOpts) {
		opts.canaryStep = canaryStep
	}
}

func WithBlueGreen(blueGreen *v1alpha1.BlueGreenStrategy) RolloutAnalysisOption {
	return func(opts *RolloutAnalysisOpts) {
		opts.blueGreen = blueGreen
	}
}

// Infix() Optional params
type InfixOpts struct {
	index *int32
}

type InfixOption func(*InfixOpts)

func InfixWithIndex(index *int32) InfixOption {
	return func(opts *InfixOpts) {
		opts.index = index
	}
}

// OutsideAnalysisBoundaries() Optional params
type OutsideAnalysisBoundariesOpts struct {
	isFullyPromoted      bool
	isJustCreated        bool
	isBeforeStartingStep bool
}

type OutsideAnalysisBoundariesOption func(*OutsideAnalysisBoundariesOpts)

func WithIsFullyPromoted(isFullyPromoted bool) OutsideAnalysisBoundariesOption {
	return func(opts *OutsideAnalysisBoundariesOpts) {
		opts.isFullyPromoted = isFullyPromoted
	}
}

func WithIsJustCreated(isJustCreated bool) OutsideAnalysisBoundariesOption {
	return func(opts *OutsideAnalysisBoundariesOpts) {
		opts.isJustCreated = isJustCreated
	}
}

func WithIsBeforeStartingStep(isBeforeStartingStep bool) OutsideAnalysisBoundariesOption {
	return func(opts *OutsideAnalysisBoundariesOpts) {
		opts.isBeforeStartingStep = isBeforeStartingStep
	}
}

type CurrentAnalysisRuns struct {
	BlueGreenPrePromotion  BlueGreenPrePromotionAR
	BlueGreenPostPromotion BlueGreenPostPromotionAR
	CanaryStep             CanaryStepAR
	CanaryBackground       CanaryBackgroundAR
}

type AnalysisContext struct {
	CurrentAnalysisRuns
	otherArs   []*v1alpha1.AnalysisRun
	log        *log.Entry
	argoClient clientset.Interface
	namespace  string
}

func NewAnalysisContext(analysisRuns []*v1alpha1.AnalysisRun, r *v1alpha1.Rollout, log *log.Entry, argoClient clientset.Interface) *AnalysisContext {
	ac := &AnalysisContext{
		CurrentAnalysisRuns: CurrentAnalysisRuns{
			BlueGreenPrePromotion: BlueGreenPrePromotionAR{
				BaseRun: BaseRun{},
			},
			BlueGreenPostPromotion: BlueGreenPostPromotionAR{
				BaseRun: BaseRun{},
			},
			CanaryStep: CanaryStepAR{
				BaseRun: BaseRun{},
			},
			CanaryBackground: CanaryBackgroundAR{
				BaseRun: BaseRun{},
			},
		},
		otherArs:   []*v1alpha1.AnalysisRun{},
		log:        log,
		argoClient: argoClient,
		namespace:  r.Namespace,
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

func (ac *AnalysisContext) UpdateCurrentAnalysisRuns(ar *v1alpha1.AnalysisRun, artype string) *AnalysisContext {
	switch artype {
	case v1alpha1.RolloutTypePrePromotionLabel:
		ac.CurrentAnalysisRuns.BlueGreenPrePromotion = BlueGreenPrePromotionAR{
			BaseRun: BaseRun{
				Run: ar,
			},
		}
	case v1alpha1.RolloutTypePostPromotionLabel:
		ac.CurrentAnalysisRuns.BlueGreenPostPromotion = BlueGreenPostPromotionAR{
			BaseRun: BaseRun{
				Run: ar,
			},
		}
	case v1alpha1.RolloutTypeStepLabel:
		ac.CurrentAnalysisRuns.CanaryStep = CanaryStepAR{
			BaseRun: BaseRun{
				Run: ar,
			},
		}
	case v1alpha1.RolloutTypeBackgroundRunLabel:
		ac.CurrentAnalysisRuns.CanaryBackground = CanaryBackgroundAR{
			BaseRun: BaseRun{
				Run: ar,
			},
		}
	}

	return ac
}

func (ac *AnalysisContext) RebuildOtherArs() {

	otherArs, _ := analysisutil.FilterAnalysisRuns(ac.otherArs, func(ar *v1alpha1.AnalysisRun) bool {
		for _, curr := range ac.CurrentAnalysisRunsToArray() {
			if ar.Name == curr.Name {
				ac.log.Infof("Rescued %s from inadvertent termination", ar.Name)
				return false
			}
		}
		return true
	})
	ac.otherArs = otherArs
}

// Due to the possibility that we are operating on stale/inconsistent data in the informer, it's
// possible that otherArs includes the current analysis runs that we just created or reclaimed
// in newCurrentAnalysisRuns, despite the fact that our rollout status did not have those set.
// To prevent us from terminating the runs that we just created moments ago, rebuild otherArs
// to ensure it does not include the newly created runs.
func (ac *AnalysisContext) cancelOtherArs() error {
	ac.RebuildOtherArs()
	for _, ar := range ac.otherArs {
		err := ac.cancelAnalysisRun(ar)
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *AnalysisContext) AllCurrentAnalysisRuns() []CurrentAnalysisRun {
	return []CurrentAnalysisRun{
		&c.BlueGreenPrePromotion,
		&c.BlueGreenPostPromotion,
		&c.CanaryStep,
		&c.CanaryBackground,
	}
}

func (c *AnalysisContext) CurrentAnalysisRunsToArray() []*v1alpha1.AnalysisRun {
	currentAnalysisRuns := []*v1alpha1.AnalysisRun{}
	if c.BlueGreenPrePromotion.Run != nil {
		currentAnalysisRuns = append(currentAnalysisRuns, c.BlueGreenPrePromotion.Run)
	}
	if c.BlueGreenPostPromotion.Run != nil {
		currentAnalysisRuns = append(currentAnalysisRuns, c.BlueGreenPostPromotion.Run)
	}
	if c.CanaryStep.Run != nil {
		currentAnalysisRuns = append(currentAnalysisRuns, c.CanaryStep.Run)
	}
	if c.CanaryBackground.Run != nil {
		currentAnalysisRuns = append(currentAnalysisRuns, c.CanaryBackground.Run)
	}
	return currentAnalysisRuns
}

func (c *AnalysisContext) AllAnalysisRuns() []*v1alpha1.AnalysisRun {
	return append(c.CurrentAnalysisRunsToArray(), c.otherArs...)
}

func (ac *AnalysisContext) BlueGreenPrePromotionAR() *v1alpha1.AnalysisRun {
	return ac.BlueGreenPrePromotion.AnalysisRun()
}

func (ac *AnalysisContext) BlueGreenPostPromotionAR() *v1alpha1.AnalysisRun {
	return ac.BlueGreenPostPromotion.AnalysisRun()
}

func (ac *AnalysisContext) CanaryStepAR() *v1alpha1.AnalysisRun {
	return ac.CanaryStep.AnalysisRun()
}

func (ac *AnalysisContext) CanaryBackgroundAR() *v1alpha1.AnalysisRun {
	return ac.CanaryBackground.AnalysisRun()
}

// could make this include the boolean check in the return; see rollout/context.go
func (ac *AnalysisContext) BlueGreenPrePromotionARStatus() *v1alpha1.RolloutAnalysisRunStatus {
	return ac.BlueGreenPrePromotion.CurrentStatus()
}

func (ac *AnalysisContext) BlueGreenPostPromotionARStatus() *v1alpha1.RolloutAnalysisRunStatus {
	return ac.BlueGreenPostPromotion.CurrentStatus()
}

func (ac *AnalysisContext) CanaryStepARStatus() *v1alpha1.RolloutAnalysisRunStatus {
	return ac.CanaryStep.CurrentStatus()
}

func (ac *AnalysisContext) CanaryBackgroundARStatus() *v1alpha1.RolloutAnalysisRunStatus {
	return ac.CanaryBackground.CurrentStatus()
}

func (ac *AnalysisContext) cancelAnalysisRuns() error {
	ctx := context.TODO()
	for _, ar := range ac.AllAnalysisRuns() {
		isNotCompleted := ar == nil || !ar.Status.Phase.Completed()
		if !ar.Spec.Terminate && isNotCompleted {
			ac.log.WithField(logutil.AnalysisRunKey, ar.Name).Infof("Canceling the analysis run '%s'", ar.Name)
			_, err := ac.argoClient.ArgoprojV1alpha1().AnalysisRuns(ar.Namespace).Patch(ctx, ar.Name, patchtypes.MergePatchType, []byte(cancelAnalysisRun), metav1.PatchOptions{})
			if err != nil {
				if k8serrors.IsNotFound(err) {
					ac.log.Warnf("AnalysisRun '%s' not found", ar.Name)
					continue
				}
				return err
			}
		}
	}
	return nil
}

func (ac *AnalysisContext) cancelAnalysisRun(ar *v1alpha1.AnalysisRun) error {
	ctx := context.TODO()

	if ar != nil && !ar.Spec.Terminate && !ar.Status.Phase.Completed() {
		ac.log.WithField(logutil.AnalysisRunKey, ar.Name).Infof("Canceling the analysis ar '%s'", ar.Name)
		_, err := ac.argoClient.ArgoprojV1alpha1().AnalysisRuns(ar.Namespace).Patch(ctx, ar.Name, patchtypes.MergePatchType, []byte(cancelAnalysisRun), metav1.PatchOptions{})
		if err != nil {
			return err
		}
	}
	return nil
}

func (ac *AnalysisContext) cancelCurrentAnalysisRun(ar CurrentAnalysisRun) error {
	ctx := context.TODO()
	if ar == nil {
		return nil
	}
	run := ar.AnalysisRun()

	if run != nil && !run.Spec.Terminate && !run.Status.Phase.Completed() {
		ac.log.WithField(logutil.AnalysisRunKey, run.Name).Infof("Canceling the analysis run '%s'", run.Name)
		_, err := ac.argoClient.ArgoprojV1alpha1().AnalysisRuns(run.Namespace).Patch(ctx, run.Name, patchtypes.MergePatchType, []byte(cancelAnalysisRun), metav1.PatchOptions{})
		if err != nil {
			return err
		}
	}
	return nil
}

func (ac *AnalysisContext) deleteAnalysisRuns(ars []*v1alpha1.AnalysisRun) error {
	ctx := context.TODO()
	for i := range ars {
		ar := ars[i]
		if ar.DeletionTimestamp != nil {
			continue
		}
		ac.log.Infof("Trying to cleanup analysis run '%s'", ar.Name)
		err := ac.argoClient.ArgoprojV1alpha1().AnalysisRuns(ar.Namespace).Delete(ctx, ar.Name, metav1.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

func (ac *AnalysisContext) generateARStatusChangeEvents(prevStatus *v1alpha1.RolloutAnalysisRunStatus, ar *v1alpha1.AnalysisRun, arType string) *AnalysisRunEvent {
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
func (ac *AnalysisContext) reconcileAnalysisRunStatusChanges(previousStatuses map[string]*v1alpha1.RolloutAnalysisRunStatus) []*AnalysisRunEvent {
	events := make([]*AnalysisRunEvent, 0)
	for _, run := range ac.AllCurrentAnalysisRuns() {
		if run.IsPresent() {
			event := ac.generateARStatusChangeEvents(previousStatuses[run.ARType()], run.AnalysisRun(), run.ARType())
			if event != nil {
				events = append(events, event)
			}
		}
	}
	return events
}

func (ac *AnalysisContext) createAnalysisRun(
	rolloutAnalysis *v1alpha1.RolloutAnalysis,
	newRS *appsv1.ReplicaSet,
	args []v1alpha1.Argument,
	infix string,
	labels map[string]string,
	newARFromRollout func(rolloutAnalysis *v1alpha1.RolloutAnalysis, args []v1alpha1.Argument, podHash string, infix string, labels map[string]string) (*v1alpha1.AnalysisRun, error),
) (*v1alpha1.AnalysisRun, error) {

	podHash := replicasetutil.GetPodTemplateHash(newRS)
	if podHash == "" {
		return nil, fmt.Errorf("Latest ReplicaSet '%s' has no pod hash in the labels", newRS.Name)
	}
	ar, err := newARFromRollout(rolloutAnalysis, args, podHash, infix, labels)
	if err != nil {
		return nil, err
	}
	analysisRunIf := ac.argoClient.ArgoprojV1alpha1().AnalysisRuns(ac.namespace)
	return analysisutil.CreateWithCollisionCounter(ac.log, analysisRunIf, *ar)
}
