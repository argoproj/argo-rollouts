package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
