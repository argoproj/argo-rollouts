package ingress

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	log "github.com/sirupsen/logrus"
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	ingressutil "github.com/argoproj/argo-rollouts/utils/ingress"
	jsonutil "github.com/argoproj/argo-rollouts/utils/json"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
)

func (c *Controller) syncALBIngress(ingress *ingressutil.Ingress, rollouts []*v1alpha1.Rollout) error {
	ctx := context.TODO()
	annotations := ingress.GetAnnotations()
	managedActions, err := ingressutil.NewManagedALBActions(annotations[ingressutil.ManagedActionsAnnotation])
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
				log.WithField(logutil.RolloutKey, roName).
					WithField(logutil.IngressKey, ingress.GetName()).
					WithField(logutil.NamespaceKey, ingress.GetNamespace()).
					Error(err)
				return nil
			}
			annotations := newIngress.GetAnnotations()
			annotations[actionKey] = resetALBAction
			newIngress.SetAnnotations(annotations)
		}
	}
	if !modified {
		return nil
	}
	newManagedStr := managedActions.String()
	newAnnotations := newIngress.GetAnnotations()
	newAnnotations[ingressutil.ManagedActionsAnnotation] = newManagedStr
	newIngress.SetAnnotations(newAnnotations)
	if newManagedStr == "" {
		delete(newIngress.GetAnnotations(), ingressutil.ManagedActionsAnnotation)
	}
	_, err = c.ingressWrapper.Update(ctx, ingress.GetNamespace(), newIngress)
	return err
}

func getResetALBActionStr(ingress *ingressutil.Ingress, action string) (string, error) {
	parts := strings.Split(action, ingressutil.ALBActionPrefix)
	if len(parts) != 2 {
		return "", fmt.Errorf("unable to parse action to get the service %s", action)
	}
	service := parts[1]

	annotations := ingress.GetAnnotations()
	previousActionStr := annotations[action]
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
