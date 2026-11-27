package plugin

import (
	"bytes"
	"encoding/json"
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

func TestNewPluginClientLoggerRespectsLogLevel(t *testing.T) {
	orig := log.GetLevel()
	defer log.SetLevel(orig)

	log.SetLevel(log.WarnLevel)
	logger := NewPluginClientLogger()
	if !logger.IsWarn() {
		t.Fatalf("expected warn level logger")
	}
	if logger.IsDebug() {
		t.Fatalf("expected debug logging to be disabled at warn level")
	}
	if got := logger.GetLevel(); got != hclog.Warn {
		t.Fatalf("GetLevel() = %v, want warn", got)
	}
}

func TestLogrusHclogLoggerNamedAndFields(t *testing.T) {
	var buf bytes.Buffer
	origOut := log.StandardLogger().Out
	origFmt := log.StandardLogger().Formatter
	defer func() {
		log.SetOutput(origOut)
		log.SetFormatter(origFmt)
		log.SetLevel(log.InfoLevel)
	}()
	log.SetOutput(&buf)
	log.SetFormatter(&log.JSONFormatter{})
	log.SetLevel(log.InfoLevel)

	logger := NewPluginClientLogger().Named("dummy-plugin")
	logger.Info("starting", "path", "/plugin-bin/dummy", "pid", 42)

	var entry map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("unmarshal: %v, raw=%q", err, buf.String())
	}
	if entry["msg"] != "starting" {
		t.Fatalf("msg = %v", entry["msg"])
	}
	if entry["logger"] != "plugin.dummy-plugin" {
		t.Fatalf("logger = %v", entry["logger"])
	}
	if entry["path"] != "/plugin-bin/dummy" {
		t.Fatalf("path = %v", entry["path"])
	}
}

func TestLogrusHclogLoggerWithAndOddArgs(t *testing.T) {
	var buf bytes.Buffer
	origOut := log.StandardLogger().Out
	defer func() {
		log.SetOutput(origOut)
		log.SetLevel(log.InfoLevel)
	}()
	log.SetOutput(&buf)
	log.SetFormatter(&log.JSONFormatter{})
	log.SetLevel(log.WarnLevel)

	logger := NewPluginClientLogger().With("plugin", "trafficrouter", "orphan")
	logger.Warn("slow reconcile")

	var entry map[string]interface{}
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &entry); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if entry["plugin"] != "trafficrouter" {
		t.Fatalf("plugin field = %v", entry["plugin"])
	}
	if entry[hclog.MissingKey] != "orphan" {
		t.Fatalf("missing key = %v", entry[hclog.MissingKey])
	}
}

func TestHclogLevelToLogrusThroughLog(t *testing.T) {
	orig := log.GetLevel()
	defer log.SetLevel(orig)
	log.SetLevel(log.TraceLevel)

	l := NewPluginClientLogger()
	if l.GetLevel() != hclog.Trace {
		t.Fatalf("GetLevel() = %v, want trace", l.GetLevel())
	}
}
