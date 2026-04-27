package log

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"k8s.io/klog/v2"

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

func TestWithRolloutObject(t *testing.T) {
	buf := bytes.NewBufferString("")
	logger := log.New()
	logger.SetOutput(buf)
	var tests = []struct {
		obj  runtime.Object
		kind string
	}{
		// extracts kind from type casting
		{&v1alpha1.Rollout{ObjectMeta: metav1.ObjectMeta{Name: "test-name", Namespace: "test-ns"}}, "rollout"},
		{&v1alpha1.AnalysisRun{ObjectMeta: metav1.ObjectMeta{Name: "test-name", Namespace: "test-ns"}}, "analysisrun"},
		{&v1alpha1.AnalysisTemplate{ObjectMeta: metav1.ObjectMeta{Name: "test-name", Namespace: "test-ns"}}, "analysistemplate"},
		{&v1alpha1.ClusterAnalysisTemplate{ObjectMeta: metav1.ObjectMeta{Name: "test-name", Namespace: "test-ns"}}, "clusteranalysistemplate"},
		{&v1alpha1.Experiment{ObjectMeta: metav1.ObjectMeta{Name: "test-name", Namespace: "test-ns"}}, "experiment"},
		// extracts kind from TypeMeta and lowercases it
		{&v1alpha1.Rollout{TypeMeta: metav1.TypeMeta{Kind: "Foo"}, ObjectMeta: metav1.ObjectMeta{Name: "test-name", Namespace: "test-ns"}}, "foo"},
	}
	for _, test := range tests {
		logCtx := WithObject(test.obj)
		logCtx.Logger = logger
		logCtx.Info("Test")
		logMessage := buf.String()
		assert.True(t, strings.Contains(logMessage, "namespace=test-ns"))
		assert.True(t, strings.Contains(logMessage, fmt.Sprintf("%s=test-name", test.kind)))
	}

}

func TestKindNamespaceName(t *testing.T) {
	var tests = []struct {
		obj               runtime.Object
		expectedKind      string
		expectedNamespace string
		expectedName      string
	}{
		{
			obj: &v1alpha1.Rollout{
				TypeMeta: metav1.TypeMeta{
					Kind: "Rollout",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-name",
					Namespace: "test-ns",
				},
			},
			expectedKind:      "Rollout",
			expectedNamespace: "test-ns",
			expectedName:      "test-name",
		},
		{
			obj: &v1alpha1.AnalysisRun{
				TypeMeta: metav1.TypeMeta{
					Kind: "AnalysisRun",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-name",
					Namespace: "test-ns",
				},
			},
			expectedKind:      "AnalysisRun",
			expectedNamespace: "test-ns",
			expectedName:      "test-name",
		},
		{
			obj: &v1alpha1.AnalysisTemplate{
				TypeMeta: metav1.TypeMeta{
					Kind: "AnalysisTemplate",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-name",
					Namespace: "test-ns",
				},
			},
			expectedKind:      "AnalysisTemplate",
			expectedNamespace: "test-ns",
			expectedName:      "test-name",
		},
		{
			obj: &v1alpha1.ClusterAnalysisTemplate{
				TypeMeta: metav1.TypeMeta{
					Kind: "ClusterAnalysisTemplate",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-name",
				},
			},
			expectedKind:      "ClusterAnalysisTemplate",
			expectedNamespace: "",
			expectedName:      "test-name",
		},
		{
			obj: &v1alpha1.Experiment{
				TypeMeta: metav1.TypeMeta{
					Kind: "Experiment",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-name",
					Namespace: "test-ns",
				},
			},
			expectedKind:      "Experiment",
			expectedNamespace: "test-ns",
			expectedName:      "test-name",
		},
	}

	for _, test := range tests {
		buf := bytes.NewBufferString("")
		logger := log.New()
		logger.SetOutput(buf)

		logCtx := WithObject(test.obj)
		kind, namespace, name := KindNamespaceName(logCtx)
		assert.Equal(t, test.expectedKind, kind)
		assert.Equal(t, test.expectedNamespace, namespace)
		assert.Equal(t, test.expectedName, name)

	}
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
	run := v1alpha1.AnalysisRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-name",
			Namespace: "test-ns",
		},
	}
	entry := WithAnalysisRun(&run)
	entry.Logger.SetOutput(buf)
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
	run := v1alpha1.AnalysisRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-name",
			Namespace: "test-ns",
		},
	}
	entry := WithAnalysisRun(&run)
	entry.Logger.SetOutput(buf)
	secrets := []string{""}
	logCtx := WithRedactor(*entry, secrets)
	logCtx.Info("Test")
	logMessage := buf.String()
	assert.False(t, strings.Contains(logMessage, "*****"))
}

// TestWithRedactorNoFormatterChain verifies that repeated calls to WithRedactor do not
// grow the formatter chain on the global logger (regression test for the original bug
// where SetFormatter was called on the shared logger, causing O(N) nesting).
func TestWithRedactorNoFormatterChain(t *testing.T) {
	run := v1alpha1.AnalysisRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-name",
			Namespace: "test-ns",
		},
	}
	originalFormatter := log.StandardLogger().Formatter

	// Call WithRedactor many times, simulating repeated AnalysisRun metric executions
	for i := 0; i < 100; i++ {
		entry := WithAnalysisRun(&run)
		_ = WithRedactor(*entry, []string{"secret"})
	}

	// The global logger's formatter must not have changed
	assert.Equal(t, originalFormatter, log.StandardLogger().Formatter,
		"WithRedactor must not mutate the global logger's formatter")
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

func TestKLogLogger(t *testing.T) {
	buf := bytes.NewBufferString("")
	logger := log.New()
	logger.SetOutput(buf)
	SetKLogLogger(logger)
	defer klog.ClearLogger()
	klog.Info("Logging from klog")
	logMessage := buf.String()
	assert.Contains(t, logMessage, "Logging from klog")
}
