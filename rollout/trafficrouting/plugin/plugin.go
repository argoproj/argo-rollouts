package plugin

import (
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/plugin/client"
	"github.com/argoproj/argo-rollouts/utils/record"
	"k8s.io/client-go/kubernetes"
)

// Type holds this controller type
const Type = "RPCPlugin"

//var pluginClient *goPlugin.Client
//var plugin rpc.TrafficRouterPlugin

type ReconcilerConfig struct {
	Rollout    *v1alpha1.Rollout
	PluginName string
	Client     kubernetes.Interface
	Recorder   record.EventRecorder
}

type Reconciler struct {
	Rollout    *v1alpha1.Rollout
	PluginName string
	Client     kubernetes.Interface
	Recorder   record.EventRecorder
}

func NewReconciler(cfg *ReconcilerConfig) (*Reconciler, error) {
	var err error
	if err != nil {
		return nil, err
	}

	reconciler := &Reconciler{
		Rollout:    cfg.Rollout,
		Client:     cfg.Client,
		Recorder:   cfg.Recorder,
		PluginName: cfg.PluginName,
	}
	return reconciler, nil
}

func (r *Reconciler) UpdateHash(canaryHash, stableHash string, additionalDestinations ...v1alpha1.WeightDestination) error {
	plugin, err := client.GetTrafficPlugin(r.PluginName)
	if err != nil {
		return err
	}

	err = plugin.UpdateHash(r.Rollout, canaryHash, stableHash, additionalDestinations)
	if err.Error() != "" {
		return err
	}
	return nil
}

func (r *Reconciler) SetWeight(desiredWeight int32, additionalDestinations ...v1alpha1.WeightDestination) error {
	plugin, err := client.GetTrafficPlugin(r.PluginName)
	if err != nil {
		return err
	}

	err = plugin.SetWeight(r.Rollout, desiredWeight, additionalDestinations)
	if err.Error() != "" {
		return err
	}
	return nil
}

func (r *Reconciler) SetHeaderRoute(headerRouting *v1alpha1.SetHeaderRoute) error {
	plugin, err := client.GetTrafficPlugin(r.PluginName)
	if err != nil {
		return err
	}

	err = plugin.SetHeaderRoute(r.Rollout, headerRouting)
	if err.Error() != "" {
		return err
	}
	return nil
}

func (r *Reconciler) VerifyWeight(desiredWeight int32, additionalDestinations ...v1alpha1.WeightDestination) (*bool, error) {
	plugin, err := client.GetTrafficPlugin(r.PluginName)
	if err != nil {
		return nil, err
	}

	verified, err := plugin.VerifyWeight(r.Rollout, desiredWeight, additionalDestinations)
	if err.Error() != "" {
		return nil, err
	}
	return verified, nil
}

func (r *Reconciler) Type() string {
	return Type
}

func (r *Reconciler) SetMirrorRoute(setMirrorRoute *v1alpha1.SetMirrorRoute) error {
	plugin, err := client.GetTrafficPlugin(r.PluginName)
	if err != nil {
		return err
	}

	err = plugin.SetMirrorRoute(r.Rollout, setMirrorRoute)
	if err.Error() != "" {
		return err
	}
	return nil
}

func (r *Reconciler) RemoveManagedRoutes() error {
	plugin, err := client.GetTrafficPlugin(r.PluginName)
	if err != nil {
		return err
	}

	err = plugin.RemoveManagedRoutes(r.Rollout)
	if err.Error() != "" {
		return err
	}
	return nil
}
