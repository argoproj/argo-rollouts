package log

import (
	"bytes"
	"strings"
	"testing"

	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/stretchr/testify/assert"
)

func TestWithRollout(t *testing.T) {
	buf := bytes.NewBufferString("")
	log.SetOutput(buf)
	ro := v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-name",
			Namespace: "test-ns",
		},
	}
	logCtx := WithRollout(&ro)
	logCtx.Info("Test")
	logMessage := buf.String()
	assert.True(t, strings.Contains(logMessage, "namespace=test-ns"))
	assert.True(t, strings.Contains(logMessage, "rollout=test-name"))
}

func TestWithExperiment(t *testing.T) {
	buf := bytes.NewBufferString("")
	log.SetOutput(buf)
	ex := v1alpha1.Experiment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-name",
			Namespace: "test-ns",
		},
	}
	logCtx := WithExperiment(&ex)
	logCtx.Info("Test")
	logMessage := buf.String()
	assert.True(t, strings.Contains(logMessage, "namespace=test-ns"))
	assert.True(t, strings.Contains(logMessage, "experiment=test-name"))
}

func TestWithAnalysis(t *testing.T) {
	buf := bytes.NewBufferString("")
	log.SetOutput(buf)
	run := v1alpha1.AnalysisRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-name",
			Namespace: "test-ns",
		},
	}
	logCtx := WithAnalysisRun(&run)
	logCtx.Info("Test")
	logMessage := buf.String()
	assert.True(t, strings.Contains(logMessage, "namespace=test-ns"))
	assert.True(t, strings.Contains(logMessage, "analysisrun=test-name"))
}

func TestWithRedactor(t *testing.T) {
	buf := bytes.NewBufferString("")
	log.SetOutput(buf)
	run := v1alpha1.AnalysisRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-name",
			Namespace: "test-ns",
		},
	}
	entry := WithAnalysisRun(&run)
	secrets := []string{"test-name", "test-ns"}
	logCtx := WithRedactor(*entry, secrets)
	logCtx.Info("Test")
	logMessage := buf.String()
	assert.False(t, strings.Contains(logMessage, "namespace=test-ns"))
	assert.False(t, strings.Contains(logMessage, "analysisrun=test-name"))
	assert.True(t, strings.Contains(logMessage, "*****"))
}
