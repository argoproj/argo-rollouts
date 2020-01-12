package metric

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

func TestMarkMeasurementError(t *testing.T) {
	now := metav1.Now()
	m := v1alpha1.Measurement{
		StartedAt: &now,
	}
	err := errors.New("my error message")
	m = MarkMeasurementError(m, err)
	assert.Equal(t, err.Error(), m.Message)
	assert.NotNil(t, m.FinishedAt)
}
