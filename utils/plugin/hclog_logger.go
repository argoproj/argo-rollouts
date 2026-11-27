package plugin

import (
	"bytes"
	"fmt"
	"io"
	golog "log"
	"strings"

	"github.com/hashicorp/go-hclog"
	log "github.com/sirupsen/logrus"
)

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

func hclogLevelToLogrus(level hclog.Level) log.Level {
	switch level {
	case hclog.Trace:
		return log.TraceLevel
	case hclog.Debug:
		return log.DebugLevel
	case hclog.Info:
		return log.InfoLevel
	case hclog.Warn:
		return log.WarnLevel
	case hclog.Error:
		return log.ErrorLevel
	default:
		return log.InfoLevel
	}
}

// NewPluginClientLogger returns an hclog.Logger for go-plugin clients that writes
// through the controller's logrus logger and formatter.
func NewPluginClientLogger() hclog.Logger {
	return &logrusHclogLogger{
		name: "plugin",
	}
}

type logrusHclogLogger struct {
	name    string
	implied []interface{}
}

func (l *logrusHclogLogger) logEntry() *log.Entry {
	fields := fieldsFromArgs(l.implied...)
	if l.name != "" {
		fields["logger"] = l.name
	}
	return log.WithFields(fields)
}

func fieldsFromArgs(args ...interface{}) log.Fields {
	fields := log.Fields{}
	for i := 0; i+1 < len(args); i += 2 {
		key, ok := args[i].(string)
		if !ok {
			key = fmt.Sprint(args[i])
		}
		fields[key] = args[i+1]
	}
	if len(args)%2 == 1 {
		fields[hclog.MissingKey] = args[len(args)-1]
	}
	return fields
}

func (l *logrusHclogLogger) Log(level hclog.Level, msg string, args ...interface{}) {
	entry := l.logEntry()
	for k, v := range fieldsFromArgs(args...) {
		entry = entry.WithField(k, v)
	}
	entry.Log(hclogLevelToLogrus(level), msg)
}

func (l *logrusHclogLogger) Trace(msg string, args ...interface{}) { l.Log(hclog.Trace, msg, args...) }
func (l *logrusHclogLogger) Debug(msg string, args ...interface{}) { l.Log(hclog.Debug, msg, args...) }
func (l *logrusHclogLogger) Info(msg string, args ...interface{})  { l.Log(hclog.Info, msg, args...) }
func (l *logrusHclogLogger) Warn(msg string, args ...interface{})  { l.Log(hclog.Warn, msg, args...) }
func (l *logrusHclogLogger) Error(msg string, args ...interface{}) { l.Log(hclog.Error, msg, args...) }

func (l *logrusHclogLogger) IsTrace() bool { return log.IsLevelEnabled(log.TraceLevel) }
func (l *logrusHclogLogger) IsDebug() bool { return log.IsLevelEnabled(log.DebugLevel) }
func (l *logrusHclogLogger) IsInfo() bool  { return log.IsLevelEnabled(log.InfoLevel) }
func (l *logrusHclogLogger) IsWarn() bool  { return log.IsLevelEnabled(log.WarnLevel) }
func (l *logrusHclogLogger) IsError() bool { return log.IsLevelEnabled(log.ErrorLevel) }

func (l *logrusHclogLogger) ImpliedArgs() []interface{} {
	out := make([]interface{}, len(l.implied))
	copy(out, l.implied)
	return out
}

func (l *logrusHclogLogger) With(args ...interface{}) hclog.Logger {
	return &logrusHclogLogger{
		name:    l.name,
		implied: append(append([]interface{}{}, l.implied...), args...),
	}
}

func (l *logrusHclogLogger) Name() string {
	return l.name
}

func (l *logrusHclogLogger) Named(name string) hclog.Logger {
	newName := name
	if l.name != "" {
		newName = l.name + "." + name
	}
	return &logrusHclogLogger{
		name:    newName,
		implied: append([]interface{}{}, l.implied...),
	}
}

func (l *logrusHclogLogger) ResetNamed(name string) hclog.Logger {
	return &logrusHclogLogger{
		name:    name,
		implied: append([]interface{}{}, l.implied...),
	}
}

func (l *logrusHclogLogger) SetLevel(_ hclog.Level) {
	// Controller log level is authoritative; ignore per-logger level changes.
}

func (l *logrusHclogLogger) GetLevel() hclog.Level {
	return LogrusLevelToHclog(log.GetLevel())
}

func (l *logrusHclogLogger) StandardLogger(opts *hclog.StandardLoggerOptions) *golog.Logger {
	if opts == nil {
		opts = &hclog.StandardLoggerOptions{}
	}
	return golog.New(l.StandardWriter(opts), "", 0)
}

func (l *logrusHclogLogger) StandardWriter(opts *hclog.StandardLoggerOptions) io.Writer {
	if opts == nil {
		opts = &hclog.StandardLoggerOptions{}
	}
	return &logrusStdlogAdapter{
		logger:                   l,
		inferLevels:              opts.InferLevels,
		inferLevelsWithTimestamp: opts.InferLevelsWithTimestamp,
		forceLevel:               opts.ForceLevel,
	}
}

type logrusStdlogAdapter struct {
	logger                   *logrusHclogLogger
	inferLevels              bool
	inferLevelsWithTimestamp bool
	forceLevel               hclog.Level
}

func (s *logrusStdlogAdapter) Write(data []byte) (int, error) {
	str := string(bytes.TrimRight(data, " \t\n"))
	if str == "" {
		return len(data), nil
	}

	if s.forceLevel != hclog.NoLevel {
		_, msg := pickStdlogLevel(str)
		s.logger.Log(s.forceLevel, msg)
		return len(data), nil
	}
	if s.inferLevels {
		if s.inferLevelsWithTimestamp {
			if idx := strings.Index(str, "]"); idx != -1 && idx+1 < len(str) {
				str = strings.TrimSpace(str[idx+1:])
			}
		}
		level, msg := pickStdlogLevel(str)
		s.logger.Log(level, msg)
		return len(data), nil
	}

	s.logger.Info(str)
	return len(data), nil
}

func pickStdlogLevel(str string) (hclog.Level, string) {
	switch {
	case strings.HasPrefix(str, "[DEBUG]"):
		return hclog.Debug, strings.TrimSpace(str[7:])
	case strings.HasPrefix(str, "[TRACE]"):
		return hclog.Trace, strings.TrimSpace(str[7:])
	case strings.HasPrefix(str, "[INFO]"):
		return hclog.Info, strings.TrimSpace(str[6:])
	case strings.HasPrefix(str, "[WARN]"):
		return hclog.Warn, strings.TrimSpace(str[6:])
	case strings.HasPrefix(str, "[ERROR]"), strings.HasPrefix(str, "[ERR]"):
		if strings.HasPrefix(str, "[ERROR]") {
			return hclog.Error, strings.TrimSpace(str[7:])
		}
		return hclog.Error, strings.TrimSpace(str[5:])
	default:
		return hclog.Info, str
	}
}
