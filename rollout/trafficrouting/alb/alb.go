package alb

import (
	"fmt"
	"strconv"

	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	extensionslisters "k8s.io/client-go/listers/extensions/v1beta1"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/diff"
	ingressutil "github.com/argoproj/argo-rollouts/utils/ingress"
	jsonutil "github.com/argoproj/argo-rollouts/utils/json"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
)

const (
	// Type holds this controller type
	Type = "ALB"
)

// ReconcilerConfig describes static configuration data for the ALB Ingress reconciler
type ReconcilerConfig struct {
	Rollout        *v1alpha1.Rollout
	Client         kubernetes.Interface
	Recorder       record.EventRecorder
	ControllerKind schema.GroupVersionKind
	IngressLister  extensionslisters.IngressLister
}

// Reconciler holds required fields to reconcile ALB Ingress resources
type Reconciler struct {
	cfg ReconcilerConfig
	log *logrus.Entry
}

// NewReconciler returns a reconciler struct that brings the ALB Ingress into the desired state
func NewReconciler(cfg ReconcilerConfig) *Reconciler {
	return &Reconciler{
		cfg: cfg,
		log: logutil.WithRollout(cfg.Rollout).WithField(logutil.IngressKey, cfg.Rollout.Spec.Strategy.Canary.TrafficRouting.ALB.Ingress),
	}
}

// Type indicates this reconciler is an ALB ingress reconciler
func (r *Reconciler) Type() string {
	return Type
}

// Reconcile modifies ALB Ingress resources to reach desired state
func (r *Reconciler) Reconcile(desiredWeight int32) error {
	rollout := r.cfg.Rollout
	ingressName := rollout.Spec.Strategy.Canary.TrafficRouting.ALB.Ingress
	ingress, err := r.cfg.IngressLister.Ingresses(rollout.Namespace).Get(ingressName)
	if err != nil {
		return err
	}
	actionService := r.cfg.Rollout.Spec.Strategy.Canary.StableService
	if r.cfg.Rollout.Spec.Strategy.Canary.TrafficRouting.ALB.RootService != "" {
		actionService = r.cfg.Rollout.Spec.Strategy.Canary.TrafficRouting.ALB.RootService

	}
	port := r.cfg.Rollout.Spec.Strategy.Canary.TrafficRouting.ALB.ServicePort
	if !ingressutil.HasRuleWithService(ingress, actionService) {
		return fmt.Errorf("ingress does not have service `%s` in rules", actionService)
	}

	desired, err := getDesiredAnnotations(ingress, rollout, port, desiredWeight)
	if err != nil {
		return err
	}
	patch, modified, err := calculatePatch(ingress, desired)
	if err != nil {
		return nil
	}
	if !modified {
		r.log.Info("no changes to the ALB Ingress")
		return nil
	}
	r.log.WithField("patch", string(patch)).Debug("applying ALB Ingress patch")
	r.log.WithField("desiredWeight", desiredWeight).Info("updating ALB Ingress")
	r.cfg.Recorder.Event(r.cfg.Rollout, corev1.EventTypeNormal, "PatchingALBIngress", fmt.Sprintf("Updating Ingress `%s` to desiredWeight '%d'", ingressName, desiredWeight))
	_, err = r.cfg.Client.ExtensionsV1beta1().Ingresses(ingress.Namespace).Patch(ingress.Name, types.MergePatchType, patch)
	if err != nil {
		r.log.WithField("err", err.Error()).Error("error patching alb ingress")
		return fmt.Errorf("error patching alb ingress `%s`: %v", ingressName, err)
	}
	return nil
}

func calculatePatch(current *extensionsv1beta1.Ingress, desiredAnnotations map[string]string) ([]byte, bool, error) {
	// only compare Annotations
	return diff.CreateTwoWayMergePatch(
		&extensionsv1beta1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: current.Annotations,
			},
		},
		&extensionsv1beta1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: desiredAnnotations,
			},
		}, extensionsv1beta1.Ingress{})
}

func getForwardActionString(r *v1alpha1.Rollout, port int32, desiredWeight int32) string {
	stableService := r.Spec.Strategy.Canary.StableService
	canaryService := r.Spec.Strategy.Canary.CanaryService
	portStr := strconv.Itoa(int(port))
	action := ingressutil.ALBAction{
		Type: "forward",
		ForwardConfig: ingressutil.ALBForwardConfig{
			TargetGroups: []ingressutil.ALBTargetGroup{
				{
					ServiceName: stableService,
					ServicePort: portStr,
					Weight:      pointer.Int64Ptr(100 - int64(desiredWeight)),
				}, {
					ServiceName: canaryService,
					ServicePort: portStr,
					Weight:      pointer.Int64Ptr(int64(desiredWeight)),
				},
			},
		},
	}
	bytes := jsonutil.MustMarshal(action)
	return string(bytes)
}

func getDesiredAnnotations(current *extensionsv1beta1.Ingress, r *v1alpha1.Rollout, port int32, desiredWeight int32) (map[string]string, error) {
	desired := current.DeepCopy().Annotations
	key := ingressutil.ALBActionAnnotationKey(r)
	desired[key] = getForwardActionString(r, port, desiredWeight)
	m, err := ingressutil.NewManagedALBActions(desired[ingressutil.ManagedActionsAnnotation])
	if err != nil {
		return nil, err
	}
	m[r.Name] = key
	desired[ingressutil.ManagedActionsAnnotation] = m.String()
	return desired, nil
}
