package plugin

import (
	"testing"

	"github.com/hashicorp/go-hclog"
	log "github.com/sirupsen/logrus"
)

func TestLogrusLevelToHclog(t *testing.T) {
	tests := []struct {
		logrus log.Level
		hclog  hclog.Level
	}{
		{log.TraceLevel, hclog.Trace},
		{log.DebugLevel, hclog.Debug},
		{log.InfoLevel, hclog.Info},
		{log.WarnLevel, hclog.Warn},
		{log.ErrorLevel, hclog.Error},
		{log.FatalLevel, hclog.Error},
		{log.PanicLevel, hclog.Error},
		{log.Level(99), hclog.Info},
	}
	for _, tt := range tests {
		if got := LogrusLevelToHclog(tt.logrus); got != tt.hclog {
			t.Errorf("LogrusLevelToHclog(%v) = %v, want %v", tt.logrus, got, tt.hclog)
		}
	}
}

func TestControllerLogFormatJSON(t *testing.T) {
	tests := []struct {
		format string
		want   bool
	}{
		{"json", true},
		{"JSON", true},
		{" json ", true},
		{"text", false},
		{"", false},
	}
	for _, tt := range tests {
		SetLogFormat(tt.format)
		if got := controllerLogFormatJSON(); got != tt.want {
			t.Errorf("controllerLogFormatJSON() after SetLogFormat(%q) = %v, want %v", tt.format, got, tt.want)
		}
	}
	SetLogFormat("")
}

func TestNewPluginClientLoggerRespectsLogLevel(t *testing.T) {
	orig := log.GetLevel()
	defer log.SetLevel(orig)

	log.SetLevel(log.WarnLevel)
	logger := NewPluginClientLogger()
	if !logger.IsWarn() {
		t.Fatalf("expected warn level logger, got level that does not accept warn")
	}
	if logger.IsDebug() {
		t.Fatalf("expected debug logging to be disabled at warn level")
	}
}
