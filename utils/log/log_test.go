package log

import (
	"bytes"
	"strings"
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

func TestWithUnstructuredObject(t *testing.T) {
	buf := bytes.NewBufferString("")
	logger := log.New()
	logger.SetOutput(buf)
	ro := v1alpha1.Rollout{
		TypeMeta: metav1.TypeMeta{
			Kind: "Rollout",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-name",
			Namespace: "test-ns",
		},
	}
	obj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&ro)
	un := unstructured.Unstructured{Object: obj}
	assert.NoError(t, err)
	logCtx := WithObject(&un)
	logCtx.Logger = logger
	logCtx.Info("Test")
	logMessage := buf.String()
	assert.True(t, strings.Contains(logMessage, "namespace=test-ns"))
	assert.True(t, strings.Contains(logMessage, "rollout=test-name"))
}

func TestWithObject(t *testing.T) {
	buf := bytes.NewBufferString("")
	logger := log.New()
	logger.SetOutput(buf)
	ro := v1alpha1.Rollout{
		TypeMeta: metav1.TypeMeta{
			Kind: "Rollout",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-name",
			Namespace: "test-ns",
		},
	}
	logCtx := WithObject(&ro)
	logCtx.Logger = logger
	logCtx.Info("Test")
	logMessage := buf.String()
	assert.True(t, strings.Contains(logMessage, "namespace=test-ns"))
	assert.True(t, strings.Contains(logMessage, "rollout=test-name"))
}

func TestWithRollout(t *testing.T) {
	buf := bytes.NewBufferString("")
	logger := log.New()
	logger.SetOutput(buf)
	ro := v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-name",
			Namespace: "test-ns",
		},
	}
	logCtx := WithRollout(&ro)
	logCtx.Logger = logger
	logCtx.Info("Test")
	logMessage := buf.String()
	assert.True(t, strings.Contains(logMessage, "namespace=test-ns"))
	assert.True(t, strings.Contains(logMessage, "rollout=test-name"))
}

func TestWithExperiment(t *testing.T) {
	buf := bytes.NewBufferString("")
	logger := log.New()
	logger.SetOutput(buf)
	ex := v1alpha1.Experiment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-name",
			Namespace: "test-ns",
		},
	}
	logCtx := WithExperiment(&ex)
	logCtx.Logger = logger
	logCtx.Info("Test")
	logMessage := buf.String()
	assert.True(t, strings.Contains(logMessage, "namespace=test-ns"))
	assert.True(t, strings.Contains(logMessage, "experiment=test-name"))
}

func TestWithAnalysis(t *testing.T) {
	buf := bytes.NewBufferString("")
	logger := log.New()
	logger.SetOutput(buf)
	run := v1alpha1.AnalysisRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-name",
			Namespace: "test-ns",
		},
	}
	logCtx := WithAnalysisRun(&run)
	logCtx.Logger = logger
	logCtx.Info("Test")
	logMessage := buf.String()
	assert.True(t, strings.Contains(logMessage, "namespace=test-ns"))
	assert.True(t, strings.Contains(logMessage, "analysisrun=test-name"))
}

// TestWithRedactor verifies that WithRedactor redacts secrets in logger
func TestWithRedactor(t *testing.T) {
	buf := bytes.NewBufferString("")
	logger := log.New()
	logger.SetOutput(buf)
	run := v1alpha1.AnalysisRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-name",
			Namespace: "test-ns",
		},
	}
	entry := WithAnalysisRun(&run)
	entry.Logger = logger
	secrets := []string{"test-name", "test-ns"}
	logCtx := WithRedactor(*entry, secrets)
	logCtx.Info("Test")
	logMessage := buf.String()
	assert.False(t, strings.Contains(logMessage, "test-name"))
	assert.False(t, strings.Contains(logMessage, "test-ns"))
	assert.True(t, strings.Contains(logMessage, "*****"))
}

// TestWithRedactor verifies that WithRedactor ignores secrets that are empty strings (to prevent injection at every character in logger)
func TestWithRedactorWithEmptySecret(t *testing.T) {
	buf := bytes.NewBufferString("")
	logger := log.New()
	logger.SetOutput(buf)
	run := v1alpha1.AnalysisRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-name",
			Namespace: "test-ns",
		},
	}
	entry := WithAnalysisRun(&run)
	entry.Logger = logger
	secrets := []string{""}
	logCtx := WithRedactor(*entry, secrets)
	logCtx.Info("Test")
	logMessage := buf.String()
	assert.False(t, strings.Contains(logMessage, "*****"))
}

func TestWithVersionFields(t *testing.T) {
	buf := bytes.NewBufferString("")
	logger := log.New()
	logger.SetOutput(buf)
	ro := v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "test-name",
			Namespace:       "test-ns",
			ResourceVersion: "123",
			Generation:      2,
		},
	}
	logCtx := WithVersionFields(&log.Entry{}, &ro)
	logCtx.Logger = logger
	logCtx.Info("Test")
	logMessage := buf.String()
	assert.True(t, strings.Contains(logMessage, "generation=2"))
	assert.True(t, strings.Contains(logMessage, "resourceVersion=123"))
}
