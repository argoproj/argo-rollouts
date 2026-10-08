package client

import (
	"bytes"
	"encoding/json"
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

// TestNewPluginLoggerRespectsControllerLogFormat verifies that the go-plugin client's own
// logger mirrors the controller's configured log format. Before this fix, the plugin
// client's Logger field was always left unset, so go-plugin always fell back to its
// unstructured, hardcoded-text default logger regardless of the controller's own
// `--logformat json` setting.
func TestNewPluginLoggerRespectsControllerLogFormat(t *testing.T) {
	origFormatter := log.StandardLogger().Formatter
	defer log.StandardLogger().SetFormatter(origFormatter)

	t.Run("default text logformat leaves go-plugin's own default logger untouched", func(t *testing.T) {
		log.StandardLogger().SetFormatter(&log.TextFormatter{})
		logger := newPluginLogger()
		assert.Nil(t, logger, "expected nil so go-plugin falls back to its own default logger")
	})

	t.Run("json logformat produces a JSON plugin logger", func(t *testing.T) {
		log.StandardLogger().SetFormatter(&log.JSONFormatter{})

		var buf bytes.Buffer
		logger := newPluginLoggerWithOutput(&buf)
		assert.NotNil(t, logger)

		logger.Info("plugin log line", "plugin", "my-plugin")

		var parsed map[string]any
		err := json.Unmarshal(buf.Bytes(), &parsed)
		assert.NoError(t, err, "expected the plugin logger to emit a single JSON object, got: %s", buf.String())
		assert.Equal(t, "plugin log line", parsed["@message"])
	})
}
