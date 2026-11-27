package commands

import (
	"io"
	"os"
	"testing"

	log "github.com/sirupsen/logrus"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/argoproj/argo-rollouts/utils/version"
)

func TestMetricsPortFlagCompatibility(t *testing.T) {
	cmd := NewCommand()

	// Test that both flags are available
	metricsPortFlag := cmd.Flags().Lookup("metricsPort")
	assert.NotNil(t, metricsPortFlag, "metricsPort flag should exist")

	metricsportFlag := cmd.Flags().Lookup("metricsport")
	assert.NotNil(t, metricsportFlag, "metricsport flag should exist for backward compatibility")

	// Test that deprecated flag is marked as deprecated
	assert.True(t, metricsportFlag.Deprecated != "", "metricsport flag should be marked as deprecated")
}

func TestNewCommandFlagDefaults(t *testing.T) {
	cmd := NewCommand()
	flags := cmd.Flags()

	tests := map[string]string{
		"rollout-resync":                 "900",
		"namespaced":                     "false",
		"loglevel":                       "info",
		"metricsPort":                    "8090",
		"healthzPort":                    "8080",
		"rollout-threads":                "10",
		"analysis-threads":               "30",
		"leader-elect":                   "true",
		"leader-election-lease-duration": "15s",
		"leader-election-renew-deadline": "10s",
		"leader-election-retry-period":   "2s",
		"analysis-shared-secret-mode":    "LabelFilter",
		"istio-api-version":              "v1alpha3",
		"traefik-api-group":              "traefik.io",
	}
	for name, want := range tests {
		f := flags.Lookup(name)
		require.NotNil(t, f, "flag %s should exist", name)
		assert.Equal(t, want, f.DefValue, "default of --%s", name)
	}

	// kubeconfig flags are registered as persistent flags
	assert.NotNil(t, cmd.PersistentFlags().Lookup("kubeconfig"))
	assert.NotNil(t, cmd.PersistentFlags().Lookup("namespace"))
	assert.Equal(t, cliName, cmd.Use)
}

func TestCreateFormatter(t *testing.T) {
	assert.IsType(t, &log.JSONFormatter{}, createFormatter("json"))
	assert.IsType(t, &log.JSONFormatter{}, createFormatter("JSON"))
	assert.IsType(t, &log.TextFormatter{}, createFormatter("text"))
	assert.IsType(t, &log.TextFormatter{}, createFormatter(""))
	assert.IsType(t, &log.TextFormatter{}, createFormatter("unknown"))
}

func TestGetEnabledControllers(t *testing.T) {
	enabled, err := getEnabledControllers(nil)
	require.NoError(t, err)
	assert.Empty(t, enabled)

	enabled, err = getEnabledControllers([]string{controllerAnalysis})
	require.NoError(t, err)
	assert.Equal(t, map[string]bool{controllerAnalysis: true}, enabled)

	_, err = getEnabledControllers([]string{controllerAnalysis, "rollout"})
	require.EqualError(t, err, "unsupported controller: rollout")
}

func TestSetLogLevel(t *testing.T) {
	defer log.SetLevel(log.InfoLevel)
	setLogLevel("debug")
	assert.Equal(t, log.DebugLevel, log.GetLevel())
	setLogLevel("warn")
	assert.Equal(t, log.WarnLevel, log.GetLevel())
}

func TestNewCommandPrintsVersion(t *testing.T) {
	stdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = stdout })

	cmd := NewCommand()
	cmd.SetArgs([]string{"--version"})
	require.NoError(t, cmd.Execute())

	require.NoError(t, w.Close())
	out, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Contains(t, string(out), version.GetVersion().String())
}

func TestNewCommandRejectsInvalidSharedSecretMode(t *testing.T) {
	cmd := NewCommand()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"--analysis-shared-secret-mode=Bogus"})

	err := cmd.Execute()
	require.ErrorContains(t, err, `invalid --analysis-shared-secret-mode "Bogus"`)
}
