package main

import (
	"strings"

	goPlugin "github.com/hashicorp/go-plugin"
	log "github.com/sirupsen/logrus"

	rolloutsPlugin "github.com/argoproj/argo-rollouts/rollout/trafficrouting/plugin/rpc"
	"github.com/argoproj/argo-rollouts/test/cmd/trafficrouter-phased-istio-plugin/internal/plugin"
)

var handshakeConfig = goPlugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "ARGO_ROLLOUTS_RPC_PLUGIN",
	MagicCookieValue: "trafficrouter",
}

func main() {
	logCtx := log.WithFields(log.Fields{"plugin": "trafficrouter"})

	setLogLevel("debug")
	log.SetFormatter(createFormatter("text"))

	rpcPluginImp := &plugin.RpcPlugin{
		LogCtx: logCtx,
	}
	var pluginMap = map[string]goPlugin.Plugin{
		"RpcTrafficRouterPlugin": &rolloutsPlugin.RpcTrafficRouterPlugin{Impl: rpcPluginImp},
	}

	goPlugin.Serve(&goPlugin.ServeConfig{
		HandshakeConfig: handshakeConfig,
		Plugins:         pluginMap,
	})
}

func createFormatter(logFormat string) log.Formatter {
	switch strings.ToLower(logFormat) {
	case "json":
		return &log.JSONFormatter{}
	default:
		return &log.TextFormatter{FullTimestamp: true}
	}
}

func setLogLevel(logLevel string) {
	level, err := log.ParseLevel(logLevel)
	if err != nil {
		log.Fatal(err)
	}
	log.SetLevel(level)
}
