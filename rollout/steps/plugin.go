package steps

//import (
//	"encoding/json"
//	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
//)
//
//type StepPlugin interface {
//	RunStep(rollout v1alpha1.Rollout, currentStepStatus v1alpha1.StepPluginStatuses) (json.RawMessage, error)
//	IsStepCompleted(rollout v1alpha1.Rollout, currentStatus v1alpha1.StepPluginStatuses) (bool, json.RawMessage, error)
//	Type() string
//}
//
//func NewStepPluginReconcile(currentStep *v1alpha1.CanaryStep) []StepPlugin {
//	stepPlugins := make([]StepPlugin, 0)
//	for pluginName, _ := range currentStep.RunPlugins {
//		switch pluginName {
//		case "consolelogger":
//			//stepPlugins = append(stepPlugins, consolelogger.NewConsoleLoggerStep())
//		}
//	}
//	return stepPlugins
//}
