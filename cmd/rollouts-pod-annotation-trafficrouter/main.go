package main

import (
	goPlugin "github.com/hashicorp/go-plugin"

	podannotation "github.com/argoproj/argo-rollouts/rollout/trafficrouting/plugin/podannotation"
	trafficRPC "github.com/argoproj/argo-rollouts/rollout/trafficrouting/plugin/rpc"
)

func main() {
	goPlugin.Serve(&goPlugin.ServeConfig{
		HandshakeConfig: goPlugin.HandshakeConfig{
			ProtocolVersion:  1,
			MagicCookieKey:   "ARGO_ROLLOUTS_RPC_PLUGIN",
			MagicCookieValue: "trafficrouter",
		},
		Plugins: map[string]goPlugin.Plugin{
			"RpcTrafficRouterPlugin": &trafficRPC.RpcTrafficRouterPlugin{Impl: podannotation.NewReconciler(nil)},
		},
	})
}
