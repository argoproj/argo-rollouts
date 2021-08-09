package graphite

import (
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

func newMockAPI(response *float64) mockAPI {
	return mockAPI{
		response: response,
	}
}

func newTestingMetric() v1alpha1.Metric {
	return v1alpha1.Metric{
		Name:             "foo",
		SuccessCondition: "result == 10.000",
		FailureCondition: "result != 10.000",
		Provider: v1alpha1.MetricProvider{
			Graphite: &v1alpha1.GraphiteMetric{
				Address: "http://some-graphite.foo",
				Query:   "foo=1",
			},
		},
	}
}

func TestType(t *testing.T) {
	response := 10.000
	g := NewGraphiteProvider(newMockAPI(&response), log.Entry{})
	assert.Equal(t, ProviderType, g.Type())
}

func TestRunSuccessfulEvaluation(t *testing.T) {
	response := 10.000
	g := NewGraphiteProvider(newMockAPI(&response), log.Entry{})
	measurement := g.Run(&v1alpha1.AnalysisRun{}, newTestingMetric())
	assert.NotNil(t, measurement.StartedAt)
	assert.Equal(t, "10.000", measurement.Value)
	assert.NotNil(t, measurement.FinishedAt)
	assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, measurement.Phase)
}

func TestRunFailedEvaluation(t *testing.T) {
	response := 5.000
	g := NewGraphiteProvider(newMockAPI(&response), log.Entry{})
	measurement := g.Run(&v1alpha1.AnalysisRun{}, newTestingMetric())
	assert.NotNil(t, measurement.StartedAt)
	assert.Equal(t, "5.000", measurement.Value)
	assert.NotNil(t, measurement.FinishedAt)
	assert.Equal(t, v1alpha1.AnalysisPhaseFailed, measurement.Phase)
}

func TestRunErrorEvaluationFromNilQueryResponse(t *testing.T) {
	g := NewGraphiteProvider(newMockAPI(nil), log.Entry{})
	measurement := g.Run(&v1alpha1.AnalysisRun{}, newTestingMetric())
	assert.NotNil(t, measurement.StartedAt)
	assert.Equal(t, "", measurement.Value)
	assert.NotNil(t, measurement.FinishedAt)
	assert.Equal(t, v1alpha1.AnalysisPhaseError, measurement.Phase)
	assert.Equal(t, "no values found", measurement.Message)
}

func TestGarbageCollect(t *testing.T) {
	response := 1.000
	g := NewGraphiteProvider(newMockAPI(&response), log.Entry{})
	err := g.GarbageCollect(nil, v1alpha1.Metric{}, 0)
	assert.NoError(t, err)
}
