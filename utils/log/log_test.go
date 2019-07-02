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
