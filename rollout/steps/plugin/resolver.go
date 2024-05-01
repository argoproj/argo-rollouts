package plugin

import (
	"fmt"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rollout/steps/plugin/client"
	"github.com/argoproj/argo-rollouts/utils/config"
	"github.com/argoproj/argo-rollouts/utils/plugin/types"
	"github.com/sirupsen/logrus"
	log "github.com/sirupsen/logrus"
)

type resolver struct {
}

type Resolver interface {
	Resolve(index int32, plugin v1alpha1.PluginStep, log *log.Entry) (StepPlugin, error)
}

func NewResolver() Resolver {
	return &resolver{}
}

func (r *resolver) Resolve(index int32, plugin v1alpha1.PluginStep, log *log.Entry) (StepPlugin, error) {
	if config, err := config.GetConfig(); err != nil {
		return nil, fmt.Errorf("could not get config: %w", err)
	} else {
		plugin := config.GetPlugin(plugin.Name, types.PluginTypeStep)
		if plugin != nil && plugin.Disabled {
			return &disabledStepPlugin{
				index: index,
				name:  plugin.Name,
			}, nil
		}
	}

	pluginClient, err := client.GetPlugin(plugin.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to get step plugin %s: %w", plugin.Name, err)
	}

	return &stepPlugin{
		rpc:    pluginClient,
		index:  index,
		name:   plugin.Name,
		config: plugin.Config,
		log:    log.WithFields(logrus.Fields{"stepplugin": plugin.Name, "stepindex": index}),
	}, nil
}
