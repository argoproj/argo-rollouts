package plugin

import (
	"os/exec"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/plugin/rpc"
	"github.com/argoproj/argo-rollouts/utils/record"
	goPlugin "github.com/hashicorp/go-plugin"
	"k8s.io/client-go/kubernetes"
)

// Type holds this controller type
const Type = "RPCPlugin"

var pluginClient *goPlugin.Client
var plugin rpc.TrafficRouterPlugin

type ReconcilerConfig struct {
	Rollout  *v1alpha1.Rollout
	Client   kubernetes.Interface
	Recorder record.EventRecorder
}

type Reconciler struct {
	Rollout  *v1alpha1.Rollout
	Client   kubernetes.Interface
	Recorder record.EventRecorder
}

func NewReconciler(cfg *ReconcilerConfig) (*Reconciler, error) {
	var err error
	pluginClient, plugin, err = startPlugin()
	if err != nil {
		return nil, err
	}
	reconciler := &Reconciler{
		Rollout:  cfg.Rollout,
		Client:   cfg.Client,
		Recorder: cfg.Recorder,
	}
	return reconciler, nil
}

func startPlugin() (*goPlugin.Client, rpc.TrafficRouterPlugin, error) {
	//if defaults.GetMetricPluginLocation() == "" {
	//	return nil, nil, fmt.Errorf("no plugin location specified")
	//}

	var handshakeConfig = goPlugin.HandshakeConfig{
		ProtocolVersion:  1,
		MagicCookieKey:   "ARGO_ROLLOUTS_RPC_PLUGIN",
		MagicCookieValue: "trafficrouter",
	}

	// pluginMap is the map of plugins we can dispense.
	var pluginMap = map[string]goPlugin.Plugin{
		"RpcTrafficRouterPlugin": &rpc.RpcTrafficRouterPlugin{},
	}

	if pluginClient == nil || pluginClient.Exited() {
		pluginClient = goPlugin.NewClient(&goPlugin.ClientConfig{
			HandshakeConfig:  handshakeConfig,
			Plugins:          pluginMap,
			VersionedPlugins: nil,
			Cmd:              exec.Command("/Users/zaller/Development/argo-rollouts/traffic-router-plugin"),
			Managed:          true,
		})

		rpcClient, err := pluginClient.Client()
		if err != nil {
			return nil, nil, err
		}

		// Request the plugin
		raw, err := rpcClient.Dispense("RpcTrafficRouterPlugin")
		if err != nil {
			return nil, nil, err
		}

		plugin = raw.(rpc.TrafficRouterPlugin)

		err = plugin.NewTrafficRouterPlugin()
		if err.Error() != "" {
			return nil, nil, err
		}
	}

	return pluginClient, plugin, nil
}

func (r *Reconciler) UpdateHash(canaryHash, stableHash string, additionalDestinations ...v1alpha1.WeightDestination) error {
	err := plugin.UpdateHash(r.Rollout, canaryHash, stableHash, additionalDestinations)
	if err.Error() != "" {
		return err
	}
	return nil
}

func (r *Reconciler) SetWeight(desiredWeight int32, additionalDestinations ...v1alpha1.WeightDestination) error {
	err := plugin.SetWeight(r.Rollout, desiredWeight, additionalDestinations)
	if err.Error() != "" {
		return err
	}
	return nil
}

func (r *Reconciler) SetHeaderRoute(headerRouting *v1alpha1.SetHeaderRoute) error {
	err := plugin.SetHeaderRoute(r.Rollout, headerRouting)
	if err.Error() != "" {
		return err
	}
	return nil
}

func (r *Reconciler) VerifyWeight(desiredWeight int32, additionalDestinations ...v1alpha1.WeightDestination) (*bool, error) {
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
	err := plugin.SetMirrorRoute(r.Rollout, setMirrorRoute)
	if err.Error() != "" {
		return err
	}
	return nil
}

func (r *Reconciler) RemoveManagedRoutes() error {
	err := plugin.RemoveManagedRoutes(r.Rollout)
	if err.Error() != "" {
		return err
	}
	return nil
}
