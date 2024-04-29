package rollout

//
//import (
//	"fmt"
//	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
//	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
//)
//
//type stepContext struct {
//	rollout           *v1alpha1.Rollout
//	pluginType        string
//	calledAt          metav1.Time
//	currentStepStatus v1alpha1.StepPluginStatuses
//}
//
//func newStepContext(rollout *v1alpha1.Rollout, pluginType string) *stepContext {
//	sc := &stepContext{
//		rollout:    rollout,
//		pluginType: pluginType,
//	}
//
//	//mapStatuses := map[string]v1alpha1.StepPluginStatuses{}
//	//json.Unmarshal(rollout.Status.SPluginStatus, &mapStatuses)
//	for _, status := range sc.rollout.Status.StepPluginStatus {
//		if status.Name == fmt.Sprintf("%s.%d", pluginType, *rollout.Status.CurrentStepIndex) {
//			sc.currentStepStatus = status
//		}
//	}
//
//	return sc
//}
//
//func (c *rolloutContext) reconcileStepPlugins() error {
//
//
//	//currentStep, index := replicasetutil.GetCurrentCanaryStep(c.rollout)
//	//if currentStep == nil {
//	//	return nil
//	//}
//	//if index == nil {
//	//	var ii int32 = 0
//	//	index = &ii
//	//}
//	//revision, revisionFound := annotations.GetRevisionAnnotation(c.rollout)
//	//if currentStep != nil && (revisionFound && revision <= 1) {
//	//	log.Printf("Skipping Step Plugin Reconcile for Rollout %s/%s, revision %d", c.rollout.Namespace, c.rollout.Name, revision)
//	//	return nil
//	//}
//	//
//	//sps := steps.NewStepPluginReconcile(currentStep)
//	//for _, plugin := range sps {
//	//	c.stepContext = newStepContext(c.rollout, plugin.Type())
//	//
//	//	c.newStatus = *c.rollout.Status.DeepCopy()
//	//
//	//	if c.controllerStartTime.After(c.stepContext.calledAt.Time) {
//	//		log.Printf("Controller Running Step: %d,  Plugin: %s", *index, plugin.Type())
//	//		res, e := plugin.RunStep(*c.rollout, c.stepContext.currentStepStatus)
//	//		c.stepContext.currentStepStatus.HasBeenCalled = true
//	//		c.stepContext.currentStepStatus.CalledAt = metav1.Now()
//	//		if e != nil {
//	//			fmt.Println(e)
//	//		}
//	//
//	//		if c.stepContext.currentStepStatus.IsEmpty() {
//	//			c.stepContext.currentStepStatus = v1alpha1.StepPluginStatuses{
//	//				Name:      fmt.Sprintf("%s.%d", plugin.Type(), *index),
//	//				StepIndex: index,
//	//				Status:    v1alpha1.Object{Value: res},
//	//			}
//	//		}
//	//		if res != nil {
//	//			c.stepContext.currentStepStatus.Status = v1alpha1.Object{Value: res}
//	//		}
//	//	}
//	//
//	//	c.newStatus.StepPluginStatus[fmt.Sprintf("%s.%d", plugin.Type(), *index)] = c.stepContext.currentStepStatus
//	//}
//
//	return nil
//}
//
//func (c *rolloutContext) setStepCondition(pluginType string, status v1alpha1.StepPluginStatuses) {
//	//c.newStatus = *c.rollout.Status.DeepCopy()
//	//if c.newStatus.SPluginStatus == nil {
//	//	c.newStatus.SPluginStatus = map[string]v1alpha1.StepPluginStatuses{}
//	//}
//	//c.newStatus.SPluginStatus[fmt.Sprintf("%s.%d", pluginType, *c.rollout.Status.CurrentStepIndex)] = status
//	//c.syncRolloutStatusCanary()
//}
