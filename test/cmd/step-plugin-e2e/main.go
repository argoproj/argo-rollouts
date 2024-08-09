package main

import (
	"strings"

	rolloutsPlugin "github.com/argoproj/argo-rollouts/rollout/steps/plugin/rpc"
	"github.com/argoproj/argo-rollouts/test/cmd/step-plugin-e2e/internal/plugin"
	goPlugin "github.com/hashicorp/go-plugin"
	log "github.com/sirupsen/logrus"
)

var handshakeConfig = goPlugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "ARGO_ROLLOUTS_RPC_PLUGIN",
	MagicCookieValue: "step",
}

func main() {
	logCtx := log.WithFields(log.Fields{"plugin": "e2e"})

	setLogLevel("debug")
	log.SetFormatter(createFormatter("text"))

	// pluginMap is the map of plugins we can dispense.
	var pluginMap = map[string]goPlugin.Plugin{
		"RpcStepPlugin": &rolloutsPlugin.RpcStepPlugin{Impl: plugin.New(logCtx)},
	}

	goPlugin.Serve(&goPlugin.ServeConfig{
		HandshakeConfig: handshakeConfig,
		Plugins:         pluginMap,
	})
}

func createFormatter(logFormat string) log.Formatter {
	var formatType log.Formatter
	switch strings.ToLower(logFormat) {
	case "json":
		formatType = &log.JSONFormatter{}
	case "text":
		formatType = &log.TextFormatter{
			FullTimestamp: true,
		}
	default:
		log.Infof("Unknown format: %s. Using text logformat", logFormat)
		formatType = &log.TextFormatter{
			FullTimestamp: true,
		}
	}

	return formatType
}

func setLogLevel(logLevel string) {
	level, err := log.ParseLevel(logLevel)
	if err != nil {
		log.Fatal(err)
	}
	log.SetLevel(level)
}
