package graphite

import (
	"errors"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

func newMockAPI(response *float64, err error) mockAPI {
	dps := []dataPoint{{
		Value:     response,
		TimeStamp: time.Now(),
	}}

	if response == nil {
		dps = []dataPoint{}
	}

	return mockAPI{
		response: dps,
		err:      err,
	}
}

func newTestingMetric() v1alpha1.Metric {
	return v1alpha1.Metric{
		Name:             "foo",
		SuccessCondition: "10.000000 in result",
		FailureCondition: "10.000000 not in result",
		Provider: v1alpha1.MetricProvider{
			Graphite: &v1alpha1.GraphiteMetric{
				Address: "http://some-graphite.foo",
				Query:   "foo=1",
			},
		},
	}
}

func TestType(t *testing.T) {
	response := 10.000000
	g := NewGraphiteProvider(newMockAPI(&response, nil), log.Entry{})
	assert.Equal(t, ProviderType, g.Type())
}

func TestRunSuccessfulEvaluation(t *testing.T) {
	response := 10.000000
	g := NewGraphiteProvider(newMockAPI(&response, nil), log.Entry{})
	metricsMetadata := g.GetMetadata(newTestingMetric())
	assert.Nil(t, metricsMetadata)

	measurement := g.Run(&v1alpha1.AnalysisRun{}, newTestingMetric())
	assert.NotNil(t, measurement.StartedAt)
	assert.Equal(t, "[10.000000]", measurement.Value)
	assert.NotNil(t, measurement.FinishedAt)
	assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, measurement.Phase)
}

func TestRunFailedEvaluation(t *testing.T) {
	response := 5.000000
	g := NewGraphiteProvider(newMockAPI(&response, nil), log.Entry{})
	metricsMetadata := g.GetMetadata(newTestingMetric())
	assert.Nil(t, metricsMetadata)

	measurement := g.Run(&v1alpha1.AnalysisRun{}, newTestingMetric())
	assert.NotNil(t, measurement.StartedAt)
	assert.Equal(t, "[5.000000]", measurement.Value)
	assert.NotNil(t, measurement.FinishedAt)
	assert.Equal(t, v1alpha1.AnalysisPhaseFailed, measurement.Phase)
}

func TestRunMeasurementError(t *testing.T) {
	metric := v1alpha1.Metric{
		Name: "foo",
		// Malformed Success and Failure Conditions
		SuccessCondition: "result[0] 10.000000",
		FailureCondition: "result[0] 10.000000",
		Provider: v1alpha1.MetricProvider{
			Graphite: &v1alpha1.GraphiteMetric{
				Address: "http://some-graphite.foo",
				Query:   "foo=1",
			},
		},
	}
	response := 10.000000
	g := NewGraphiteProvider(newMockAPI(&response, nil), log.Entry{})
	measurement := g.Run(&v1alpha1.AnalysisRun{}, metric)
	assert.NotNil(t, measurement.StartedAt)
	assert.Equal(t, "[10.000000]", measurement.Value)
	assert.NotNil(t, measurement.FinishedAt)
	assert.Equal(t, v1alpha1.AnalysisPhaseError, measurement.Phase)
	assert.Equal(t, "could not evaluate successCondition \"result[0] 10.000000\": unexpected token Number(\"10.000000\")", measurement.Message)
}

func TestRunEmptyResultIsEvaluatedByConditions(t *testing.T) {
	newMetric := func(successCondition, failureCondition string) v1alpha1.Metric {
		metric := newTestingMetric()
		metric.SuccessCondition = successCondition
		metric.FailureCondition = failureCondition
		return metric
	}
	tests := []struct {
		name             string
		successCondition string
		failureCondition string
		expectedPhase    v1alpha1.AnalysisPhase
		expectedMessage  string
	}{
		{
			// An empty result means there is nothing to report (e.g. no errors in the last minutes),
			// so a condition that handles an empty result must be able to succeed.
			name:             "condition handling an empty result succeeds",
			successCondition: "len(result) > 0 ? result[0] < 1 : true",
			expectedPhase:    v1alpha1.AnalysisPhaseSuccessful,
		},
		{
			name:             "condition handling an empty result can fail",
			successCondition: "len(result) > 0",
			expectedPhase:    v1alpha1.AnalysisPhaseFailed,
		},
		{
			name:             "membership conditions are evaluated",
			successCondition: "10.000000 in result",
			failureCondition: "10.000000 not in result",
			expectedPhase:    v1alpha1.AnalysisPhaseFailed,
		},
		{
			// Conditions that index into the result keep being an error when there is no value.
			name:             "condition indexing an empty result is an error",
			successCondition: "result[0] < 10",
			expectedPhase:    v1alpha1.AnalysisPhaseError,
			expectedMessage:  "could not evaluate successCondition",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			g := NewGraphiteProvider(newMockAPI(nil, nil), log.Entry{})
			measurement := g.Run(&v1alpha1.AnalysisRun{}, newMetric(test.successCondition, test.failureCondition))
			assert.NotNil(t, measurement.StartedAt)
			assert.NotNil(t, measurement.FinishedAt)
			assert.Equal(t, test.expectedPhase, measurement.Phase)
			assert.Contains(t, measurement.Message, test.expectedMessage)
			if test.expectedPhase != v1alpha1.AnalysisPhaseError {
				assert.Equal(t, "[]", measurement.Value)
			}
		})
	}
}

func TestRunErrorEvaluationFromErrorQueryResponse(t *testing.T) {
	response := 10.000000
	g := NewGraphiteProvider(newMockAPI(&response, errors.New("some err")), log.Entry{})
	measurement := g.Run(&v1alpha1.AnalysisRun{}, newTestingMetric())
	assert.NotNil(t, measurement.StartedAt)
	assert.Equal(t, "", measurement.Value)
	assert.NotNil(t, measurement.FinishedAt)
	assert.Equal(t, v1alpha1.AnalysisPhaseError, measurement.Phase)
	assert.Equal(t, "some err", measurement.Message)
}

func TestResume(t *testing.T) {
	response := 1.000000
	e := log.NewEntry(log.New())
	g := NewGraphiteProvider(newMockAPI(&response, nil), *e)
	metric := newTestingMetric()
	analysisRun := &v1alpha1.AnalysisRun{}
	measurement := g.Run(analysisRun, metric)
	m := g.Resume(nil, metric, measurement)
	assert.Equal(t, m, measurement)
}

func TestTerminate(t *testing.T) {
	response := 1.000000
	e := log.NewEntry(log.New())
	g := NewGraphiteProvider(newMockAPI(&response, nil), *e)
	metric := newTestingMetric()
	analysisRun := &v1alpha1.AnalysisRun{}
	measurement := g.Run(analysisRun, metric)
	m := g.Terminate(nil, metric, measurement)
	assert.Equal(t, m, measurement)
}

func TestGarbageCollect(t *testing.T) {
	response := 1.000000
	g := NewGraphiteProvider(newMockAPI(&response, nil), log.Entry{})
	err := g.GarbageCollect(nil, v1alpha1.Metric{}, 0)
	assert.NoError(t, err)
}
