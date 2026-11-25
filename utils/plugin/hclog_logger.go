package plugin

import (
	"os"
	"strings"
	"sync"

	"github.com/hashicorp/go-hclog"
	log "github.com/sirupsen/logrus"
)

const jsonLogFormat = "json"

var (
	controllerLogFormatMu sync.RWMutex
	controllerLogFormat   string
)

// SetLogFormat configures the log format used for go-plugin client loggers.
// Call during controller startup after parsing --logformat.
func SetLogFormat(format string) {
	controllerLogFormatMu.Lock()
	defer controllerLogFormatMu.Unlock()
	controllerLogFormat = format
}

func controllerLogFormatJSON() bool {
	controllerLogFormatMu.RLock()
	defer controllerLogFormatMu.RUnlock()
	return strings.EqualFold(strings.TrimSpace(controllerLogFormat), jsonLogFormat)
}

// LogrusLevelToHclog maps the controller logrus level to the corresponding hclog level.
func LogrusLevelToHclog(level log.Level) hclog.Level {
	switch level {
	case log.TraceLevel:
		return hclog.Trace
	case log.DebugLevel:
		return hclog.Debug
	case log.InfoLevel:
		return hclog.Info
	case log.WarnLevel:
		return hclog.Warn
	case log.ErrorLevel, log.FatalLevel, log.PanicLevel:
		return hclog.Error
	default:
		return hclog.Info
	}
}

// NewPluginClientLogger returns an hclog.Logger for go-plugin clients that follows
// the controller's log level and format.
func NewPluginClientLogger() hclog.Logger {
	output := log.StandardLogger().Out
	if output == nil {
		output = os.Stdout
	}

	return hclog.New(&hclog.LoggerOptions{
		Name:       "plugin",
		Output:     output,
		Level:      LogrusLevelToHclog(log.GetLevel()),
		JSONFormat: controllerLogFormatJSON(),
	})
}
