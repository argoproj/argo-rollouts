package ingress

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	log "github.com/sirupsen/logrus"
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	ingressutil "github.com/argoproj/argo-rollouts/utils/ingress"
	jsonutil "github.com/argoproj/argo-rollouts/utils/json"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
)

func (c *Controller) syncALBIngress(ingress *extensionsv1beta1.Ingress, rollouts []*v1alpha1.Rollout) error {
	ctx := context.TODO()
	managedActions, err := ingressutil.NewManagedALBActions(ingress.Annotations[ingressutil.ManagedActionsAnnotation])
	if err != nil {
		return nil
	}
	actionHasExistingRollout := map[string]bool{}
	for i := range rollouts {
		rollout := rollouts[i]
		if _, ok := managedActions[rollout.Name]; ok {
			actionHasExistingRollout[rollout.Name] = true
			c.enqueueRollout(rollout)
		}
	}
	newIngress := ingress.DeepCopy()
	modified := false
	for roName := range managedActions {
		if _, ok := actionHasExistingRollout[roName]; !ok {
			modified = true
			actionKey := managedActions[roName]
			delete(managedActions, roName)
			resetALBAction, err := getResetALBActionStr(ingress, actionKey)
			if err != nil {
				log.WithField(logutil.RolloutKey, roName).WithField(logutil.IngressKey, ingress.Name).WithField(logutil.NamespaceKey, ingress.Namespace).Error(err)
				return nil
			}
			newIngress.Annotations[actionKey] = resetALBAction
		}
	}
	if !modified {
		return nil
	}
	newManagedStr := managedActions.String()
	newIngress.Annotations[ingressutil.ManagedActionsAnnotation] = newManagedStr
	if newManagedStr == "" {
		delete(newIngress.Annotations, ingressutil.ManagedActionsAnnotation)
	}
	_, err = c.client.ExtensionsV1beta1().Ingresses(ingress.Namespace).Update(ctx, newIngress, metav1.UpdateOptions{})
	return err
}

func getResetALBActionStr(ingress *extensionsv1beta1.Ingress, action string) (string, error) {
	parts := strings.Split(action, ingressutil.ALBActionPrefix)
	if len(parts) != 2 {
		return "", fmt.Errorf("unable to parse action to get the service %s", action)
	}
	service := parts[1]

	previousActionStr := ingress.Annotations[action]
	var previousAction ingressutil.ALBAction
	err := json.Unmarshal([]byte(previousActionStr), &previousAction)
	if err != nil {
		return "", fmt.Errorf("unable to unmarshal previous ALB action")
	}

	var port string
	for _, tg := range previousAction.ForwardConfig.TargetGroups {
		if tg.ServiceName == service {
			port = tg.ServicePort
		}
	}
	if port == "" {
		return "", fmt.Errorf("unable to reset annotation due to missing port")
	}

	albAction := ingressutil.ALBAction{
		Type: "forward",
		ForwardConfig: ingressutil.ALBForwardConfig{
			TargetGroups: []ingressutil.ALBTargetGroup{
				{
					ServiceName: service,
					ServicePort: port,
					Weight:      pointer.Int64Ptr(int64(100)),
				},
			},
		},
	}
	bytes := jsonutil.MustMarshal(albAction)
	return string(bytes), nil
}
