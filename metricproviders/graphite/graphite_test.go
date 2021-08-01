package graphite

import (
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

func newMockAPI(response float64) mockAPI {
	return mockAPI{
		response: &response,
	}
}

func TestRunSuccessfulEvaluation(t *testing.T) {
	e := log.Entry{}
	metric := v1alpha1.Metric{
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
	g := NewGraphiteProvider(newMockAPI(10.000), e)
	measurement := g.Run(&v1alpha1.AnalysisRun{}, metric)
	assert.NotNil(t, measurement.StartedAt)
	assert.Equal(t, "10.000", measurement.Value)
	assert.NotNil(t, measurement.FinishedAt)
	assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, measurement.Phase)
}

func TestRunFailedEvaluation(t *testing.T) {
	e := log.Entry{}
	metric := v1alpha1.Metric{
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
	g := NewGraphiteProvider(newMockAPI(5.000), e)
	measurement := g.Run(&v1alpha1.AnalysisRun{}, metric)
	assert.NotNil(t, measurement.StartedAt)
	assert.Equal(t, "5.000", measurement.Value)
	assert.NotNil(t, measurement.FinishedAt)
	assert.Equal(t, v1alpha1.AnalysisPhaseFailed, measurement.Phase)
}

func TestGarbageCollect(t *testing.T) {
	e := log.NewEntry(log.New())
	g := NewGraphiteProvider(newMockAPI(1), *e)

	err := g.GarbageCollect(nil, v1alpha1.Metric{}, 0)
	assert.NoError(t, err)
}
