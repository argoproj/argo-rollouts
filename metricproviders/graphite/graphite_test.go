package graphite

import (
	"errors"
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

func newMockAPI(response *float64, err error) mockAPI {
	return mockAPI{
		response: response,
		err:      err,
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
	g := NewGraphiteProvider(newMockAPI(&response, nil), log.Entry{})
	assert.Equal(t, ProviderType, g.Type())
}

func TestRunSuccessfulEvaluation(t *testing.T) {
	response := 10.000
	g := NewGraphiteProvider(newMockAPI(&response, nil), log.Entry{})
	measurement := g.Run(&v1alpha1.AnalysisRun{}, newTestingMetric())
	assert.NotNil(t, measurement.StartedAt)
	assert.Equal(t, "10.000", measurement.Value)
	assert.NotNil(t, measurement.FinishedAt)
	assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, measurement.Phase)
}

func TestRunFailedEvaluation(t *testing.T) {
	response := 5.000
	g := NewGraphiteProvider(newMockAPI(&response, nil), log.Entry{})
	measurement := g.Run(&v1alpha1.AnalysisRun{}, newTestingMetric())
	assert.NotNil(t, measurement.StartedAt)
	assert.Equal(t, "5.000", measurement.Value)
	assert.NotNil(t, measurement.FinishedAt)
	assert.Equal(t, v1alpha1.AnalysisPhaseFailed, measurement.Phase)
}

func TestRunMeasurementError(t *testing.T) {
	metric := v1alpha1.Metric{
		Name: "foo",
		// Malformed Success and Failure Conditions
		SuccessCondition: "result 10.000",
		FailureCondition: "result 10.000",
		Provider: v1alpha1.MetricProvider{
			Graphite: &v1alpha1.GraphiteMetric{
				Address: "http://some-graphite.foo",
				Query:   "foo=1",
			},
		},
	}
	response := 10.000
	g := NewGraphiteProvider(newMockAPI(&response, nil), log.Entry{})
	measurement := g.Run(&v1alpha1.AnalysisRun{}, metric)
	assert.NotNil(t, measurement.StartedAt)
	assert.Equal(t, "10.000", measurement.Value)
	assert.NotNil(t, measurement.FinishedAt)
	assert.Equal(t, v1alpha1.AnalysisPhaseError, measurement.Phase)
	assert.Equal(t, "unexpected token Number(\"10.000\")", measurement.Message)
}

func TestRunErrorEvaluationFromNilQueryResponse(t *testing.T) {
	g := NewGraphiteProvider(newMockAPI(nil, nil), log.Entry{})
	measurement := g.Run(&v1alpha1.AnalysisRun{}, newTestingMetric())
	assert.NotNil(t, measurement.StartedAt)
	assert.Equal(t, "", measurement.Value)
	assert.NotNil(t, measurement.FinishedAt)
	assert.Equal(t, v1alpha1.AnalysisPhaseError, measurement.Phase)
	assert.Equal(t, "no values found", measurement.Message)
}

func TestRunErrorEvaluationFromErrorQueryResponse(t *testing.T) {
	response := 10.000
	g := NewGraphiteProvider(newMockAPI(&response, errors.New("some err")), log.Entry{})
	measurement := g.Run(&v1alpha1.AnalysisRun{}, newTestingMetric())
	assert.NotNil(t, measurement.StartedAt)
	assert.Equal(t, "", measurement.Value)
	assert.NotNil(t, measurement.FinishedAt)
	assert.Equal(t, v1alpha1.AnalysisPhaseError, measurement.Phase)
	assert.Equal(t, "some err", measurement.Message)
}

func TestResume(t *testing.T) {
	response := 1.000
	e := log.NewEntry(log.New())
	g := NewGraphiteProvider(newMockAPI(&response, nil), *e)
	metric := newTestingMetric()
	analysisRun := &v1alpha1.AnalysisRun{}
	measurement := g.Run(analysisRun, metric)
	m := g.Resume(nil, metric, measurement)
	assert.Equal(t, m, measurement)
}

func TestTerminate(t *testing.T) {
	response := 1.000
	e := log.NewEntry(log.New())
	g := NewGraphiteProvider(newMockAPI(&response, nil), *e)
	metric := newTestingMetric()
	analysisRun := &v1alpha1.AnalysisRun{}
	measurement := g.Run(analysisRun, metric)
	m := g.Terminate(nil, metric, measurement)
	assert.Equal(t, m, measurement)
}

func TestGarbageCollect(t *testing.T) {
	response := 1.000
	g := NewGraphiteProvider(newMockAPI(&response, nil), log.Entry{})
	err := g.GarbageCollect(nil, v1alpha1.Metric{}, 0)
	assert.NoError(t, err)
}
