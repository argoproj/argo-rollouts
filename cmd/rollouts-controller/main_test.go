package main

import (
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace/noop"
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

func TestOpenTelemetryFlagDefaults(t *testing.T) {
	flags := newCommand().Flags()

	address, err := flags.GetString("otlp-address")
	require.NoError(t, err)
	assert.Empty(t, address)
	insecure, err := flags.GetBool("otlp-insecure")
	require.NoError(t, err)
	assert.True(t, insecure)
	headers, err := flags.GetStringToString("otlp-headers")
	require.NoError(t, err)
	assert.Empty(t, headers)
	attrs, err := flags.GetStringSlice("otlp-attrs")
	require.NoError(t, err)
	assert.Empty(t, attrs)
	ratio, err := flags.GetFloat64("otlp-sample-ratio")
	require.NoError(t, err)
	assert.Equal(t, 1.0, ratio)
}

func TestOpenTelemetryFlagValues(t *testing.T) {
	flags := newCommand().Flags()
	require.NoError(t, flags.Parse([]string{
		"--otlp-address=collector.example.com:4317",
		"--otlp-insecure=false",
		"--otlp-headers=authorization=Bearer-token,x-tenant=example",
		"--otlp-attrs=deployment.environment.name:production,endpoint:https://example.com:4317",
		"--otlp-attrs=team:platform",
		"--otlp-sample-ratio=0.25",
	}))

	address, err := flags.GetString("otlp-address")
	require.NoError(t, err)
	assert.Equal(t, "collector.example.com:4317", address)
	insecure, err := flags.GetBool("otlp-insecure")
	require.NoError(t, err)
	assert.False(t, insecure)
	headers, err := flags.GetStringToString("otlp-headers")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"authorization": "Bearer-token", "x-tenant": "example"}, headers)
	attrs, err := flags.GetStringSlice("otlp-attrs")
	require.NoError(t, err)
	assert.Equal(t, []string{
		"deployment.environment.name:production",
		"endpoint:https://example.com:4317",
		"team:platform",
	}, attrs)
	ratio, err := flags.GetFloat64("otlp-sample-ratio")
	require.NoError(t, err)
	assert.Equal(t, 0.25, ratio)
}

func TestOpenTelemetryInvalidSampleRatio(t *testing.T) {
	for _, ratio := range []string{"-0.1", "1.1", "NaN", "+Inf", "-Inf"} {
		t.Run(ratio, func(t *testing.T) {
			cmd := newCommand()
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs([]string{"--otlp-address=localhost:4317", "--otlp-sample-ratio=" + ratio})

			err := cmd.Execute()

			require.ErrorContains(t, err, "otlp-sample-ratio")
			assert.ErrorContains(t, err, "between 0 and 1")
		})
	}
}

func TestVersionDoesNotInitializeTracing(t *testing.T) {
	originalProvider := otel.GetTracerProvider()
	originalPropagator := otel.GetTextMapPropagator()
	t.Cleanup(func() {
		otel.SetTracerProvider(originalProvider)
		otel.SetTextMapPropagator(originalPropagator)
	})
	provider := noop.NewTracerProvider()
	propagator := propagation.NewCompositeTextMapPropagator(propagation.TraceContext{})
	otel.SetTracerProvider(&provider)
	otel.SetTextMapPropagator(propagator)
	cmd := newCommand()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{
		"--version",
		"--otlp-address=localhost:4317",
		"--otlp-sample-ratio=2",
		"--otlp-attrs=missing-separator",
	})

	require.NoError(t, cmd.Execute())
	assert.Same(t, &provider, otel.GetTracerProvider())
	assert.Equal(t, propagator, otel.GetTextMapPropagator())
}
