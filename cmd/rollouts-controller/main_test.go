package main

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/argoproj/argo-rollouts/utils/defaults"
)

func TestMetricsPortFlagCompatibility(t *testing.T) {
	cmd := newCommand()

	// Test that both flags are available
	metricsPortFlag := cmd.Flags().Lookup("metricsPort")
	assert.NotNil(t, metricsPortFlag, "metricsPort flag should exist")

	metricsportFlag := cmd.Flags().Lookup("metricsport")
	assert.NotNil(t, metricsportFlag, "metricsport flag should exist for backward compatibility")

	// Test that deprecated flag is marked as deprecated
	assert.True(t, metricsportFlag.Deprecated != "", "metricsport flag should be marked as deprecated")
}

func TestConfigMapNameFlag(t *testing.T) {
	cmd := newCommand()

	configmapNameFlag := cmd.Flags().Lookup("configmap-name")
	assert.NotNil(t, configmapNameFlag, "configmap-name flag should exist")
	assert.Equal(t, defaults.DefaultRolloutsConfigMapName, configmapNameFlag.DefValue, "configmap-name flag should default to %s", defaults.DefaultRolloutsConfigMapName)
}
