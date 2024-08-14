package main

import (
	"os"
	"strconv"
	"strings"
	"time"

	rolloutsPlugin "github.com/argoproj/argo-rollouts/rollout/steps/plugin/rpc"
	"github.com/argoproj/argo-rollouts/test/cmd/step-plugin-sample/internal/plugin"
	goPlugin "github.com/hashicorp/go-plugin"
	log "github.com/sirupsen/logrus"
)

// handshakeConfigs are used to just do a basic handshake between
// a plugin and host. If the handshake fails, a user friendly error is shown.
// This prevents users from executing bad plugins or executing a plugin
// directory. It is a UX feature, not a security feature.
var handshakeConfig = goPlugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "ARGO_ROLLOUTS_RPC_PLUGIN",
	MagicCookieValue: "step",
}

func main() {
	logCtx := log.WithFields(log.Fields{"plugin": "step"})

	setLogLevel("debug")
	log.SetFormatter(createFormatter("text"))

	seed := time.Now().UnixNano()

	args := os.Args[1:]
	if len(args) > 0 {
		if args[0] == "--seed" {
			if len(args) >= 2 {
				n, err := strconv.ParseInt(args[1], 10, 64)
				if err != nil {
					log.Fatal(err)
				}
				seed = n
			} else {
				log.Fatal("No value for --seed argument")
			}
		}
	}

	// pluginMap is the map of plugins we can dispense.
	var pluginMap = map[string]goPlugin.Plugin{
		"RpcStepPlugin": &rolloutsPlugin.RpcStepPlugin{Impl: plugin.New(logCtx, seed)},
	}

	logCtx.Debug("message from plugin", "foo", "bar")

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
