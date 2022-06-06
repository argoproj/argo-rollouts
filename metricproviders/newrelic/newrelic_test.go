package newrelic

import (
	"fmt"
	"os"
	"testing"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/newrelic/newrelic-client-go/pkg/nrdb"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newAnalysisRun() *v1alpha1.AnalysisRun {
	return &v1alpha1.AnalysisRun{}
}

func TestType(t *testing.T) {
	e := log.Entry{}
	mock := &mockAPI{}
	p := NewNewRelicProvider(mock, e)
	assert.Equal(t, ProviderType, p.Type())
}

func TestRunSuccessfully(t *testing.T) {
	e := log.Entry{}
	mock := &mockAPI{
		response: []nrdb.NRDBResult{map[string]interface{}{"count": 10}},
	}
	p := NewNewRelicProvider(mock, e)
	metric := v1alpha1.Metric{
		Name:             "foo",
		SuccessCondition: "result.count == 10",
		FailureCondition: "result.count != 10",
		Provider: v1alpha1.MetricProvider{
			NewRelic: &v1alpha1.NewRelicMetric{
				Query: "test",
			},
		},
	}
	metricsMetadata := p.GetMetadata(metric)
	assert.Nil(t, metricsMetadata)

	measurement := p.Run(newAnalysisRun(), metric)
	assert.NotNil(t, measurement.StartedAt)
	assert.Equal(t, `{"count":10}`, measurement.Value)
	assert.NotNil(t, measurement.FinishedAt)
	assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, measurement.Phase)
}

func TestRunWithTimeseries(t *testing.T) {
	e := log.NewEntry(log.New())
	mock := &mockAPI{
		response: []nrdb.NRDBResult{
			map[string]interface{}{"count": 10},
			map[string]interface{}{"count": 20},
			map[string]interface{}{"count": 30}},
	}
	p := NewNewRelicProvider(mock, *e)
	metric := v1alpha1.Metric{
		Name:             "foo",
		SuccessCondition: "result[0].count == 10",
		FailureCondition: "result[2].count < 20",
		Provider: v1alpha1.MetricProvider{
			NewRelic: &v1alpha1.NewRelicMetric{
				Query: "test",
			},
		},
	}
	metricsMetadata := p.GetMetadata(metric)
	assert.Nil(t, metricsMetadata)

	measurement := p.Run(newAnalysisRun(), metric)
	assert.NotNil(t, measurement.StartedAt)
	assert.Equal(t, `[{"count":10},{"count":20},{"count":30}]`, measurement.Value)
	assert.NotNil(t, measurement.FinishedAt)
	assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, measurement.Phase)
}

func TestRunWithFacet(t *testing.T) {
	e := log.NewEntry(log.New())
	mock := &mockAPI{
		response: []nrdb.NRDBResult{map[string]interface{}{"count": 10, "average.duration": 12.34}},
	}
	p := NewNewRelicProvider(mock, *e)
	metric := v1alpha1.Metric{
		Name:             "foo",
		SuccessCondition: "result.count == 10 and result['average.duration'] < 15.0",
		FailureCondition: "result.count != 10 or result['average.duration'] >= 15.0",
		Provider: v1alpha1.MetricProvider{
			NewRelic: &v1alpha1.NewRelicMetric{
				Query: "test",
			},
		},
	}
	metricsMetadata := p.GetMetadata(metric)
	assert.Nil(t, metricsMetadata)

	measurement := p.Run(newAnalysisRun(), metric)
	assert.NotNil(t, measurement.StartedAt)
	assert.Equal(t, `{"average.duration":12.34,"count":10}`, measurement.Value)
	assert.NotNil(t, measurement.FinishedAt)
	assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, measurement.Phase)
}

func TestRunWithMultipleSelectTerms(t *testing.T) {
	e := log.NewEntry(log.New())
	mock := &mockAPI{
		response: []nrdb.NRDBResult{map[string]interface{}{"count": 10}},
	}
	p := NewNewRelicProvider(mock, *e)
	metric := v1alpha1.Metric{
		Name:             "foo",
		SuccessCondition: "result.count == 10",
		FailureCondition: "result.count != 10",
		Provider: v1alpha1.MetricProvider{
			NewRelic: &v1alpha1.NewRelicMetric{
				Query: "test",
			},
		},
	}
	metricsMetadata := p.GetMetadata(metric)
	assert.Nil(t, metricsMetadata)

	measurement := p.Run(newAnalysisRun(), metric)
	assert.NotNil(t, measurement.StartedAt)
	assert.Equal(t, `{"count":10}`, measurement.Value)
	assert.NotNil(t, measurement.FinishedAt)
	assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, measurement.Phase)
}

func TestRunWithEmptyResult(t *testing.T) {
	e := log.NewEntry(log.New())
	expectedErr := fmt.Errorf("no results returned from NRQL query")
	mock := &mockAPI{
		response: []nrdb.NRDBResult{make(map[string]interface{})},
	}
	p := NewNewRelicProvider(mock, *e)
	metric := v1alpha1.Metric{
		Name:             "foo",
		SuccessCondition: "result.count == 10",
		FailureCondition: "result.count != 10",
		Provider: v1alpha1.MetricProvider{
			NewRelic: &v1alpha1.NewRelicMetric{
				Query: "test",
			},
		},
	}
	measurement := p.Run(newAnalysisRun(), metric)
	assert.Equal(t, expectedErr.Error(), measurement.Message)
	assert.NotNil(t, measurement.StartedAt)
	assert.Equal(t, "", measurement.Value)
	assert.NotNil(t, measurement.FinishedAt)
	assert.Equal(t, v1alpha1.AnalysisPhaseError, measurement.Phase)
}

func TestRunWithQueryError(t *testing.T) {
	e := log.NewEntry(log.New())
	expectedErr := fmt.Errorf("bad big bug :(")
	mock := &mockAPI{
		err: expectedErr,
	}
	p := NewNewRelicProvider(mock, *e)
	metric := v1alpha1.Metric{
		Name:             "foo",
		SuccessCondition: "result == 10",
		FailureCondition: "result != 10",
		Provider: v1alpha1.MetricProvider{
			NewRelic: &v1alpha1.NewRelicMetric{
				Query: "test",
			},
		},
	}
	measurement := p.Run(newAnalysisRun(), metric)
	assert.Equal(t, expectedErr.Error(), measurement.Message)
	assert.NotNil(t, measurement.StartedAt)
	assert.Equal(t, "", measurement.Value)
	assert.NotNil(t, measurement.FinishedAt)
	assert.Equal(t, v1alpha1.AnalysisPhaseError, measurement.Phase)
}

func TestRunWithResolveArgsError(t *testing.T) {
	e := log.NewEntry(log.New())
	expectedErr := fmt.Errorf("failed to resolve {{args.var}}")
	mock := &mockAPI{
		err: expectedErr,
	}
	p := NewNewRelicProvider(mock, *e)
	metric := v1alpha1.Metric{
		Name: "foo",
		Provider: v1alpha1.MetricProvider{
			NewRelic: &v1alpha1.NewRelicMetric{
				Query: "test",
			},
		},
	}
	measurement := p.Run(newAnalysisRun(), metric)
	assert.Equal(t, expectedErr.Error(), measurement.Message)
	assert.NotNil(t, measurement.StartedAt)
	assert.Equal(t, "", measurement.Value)
	assert.NotNil(t, measurement.FinishedAt)
	assert.Equal(t, v1alpha1.AnalysisPhaseError, measurement.Phase)
}

func TestRunWithEvaluationError(t *testing.T) {
	e := log.WithField("", "")
	mock := &mockAPI{}
	p := NewNewRelicProvider(mock, *e)
	metric := v1alpha1.Metric{
		Name:             "foo",
		SuccessCondition: "result == 10",
		FailureCondition: "result != 10",
		Provider: v1alpha1.MetricProvider{
			NewRelic: &v1alpha1.NewRelicMetric{
				Query: "test",
			},
		},
	}
	measurement := p.Run(newAnalysisRun(), metric)
	assert.Equal(t, "no results returned from NRQL query", measurement.Message)
	assert.NotNil(t, measurement.StartedAt)
	assert.Equal(t, "", measurement.Value)
	assert.NotNil(t, measurement.FinishedAt)
	assert.Equal(t, v1alpha1.AnalysisPhaseError, measurement.Phase)
}

// contrived example using func. Real world queries should always return types that can safely marshal to JSON
func TestRunWithInvalidJSON(t *testing.T) {
	e := log.WithField("", "")
	metric := v1alpha1.Metric{
		Name:             "foo",
		SuccessCondition: "result == 10",
		FailureCondition: "result != 10",
		Provider: v1alpha1.MetricProvider{
			NewRelic: &v1alpha1.NewRelicMetric{
				Query: "test",
			},
		},
	}
	t.Run("with a single result map", func(t *testing.T) {
		mock := &mockAPI{
			response: []nrdb.NRDBResult{map[string]interface{}{"func": func() {}}},
		}
		p := NewNewRelicProvider(mock, *e)
		measurement := p.Run(newAnalysisRun(), metric)
		assert.Equal(t, "could not marshal results: json: unsupported type: func()", measurement.Message)
		assert.NotNil(t, measurement.StartedAt)
		assert.Equal(t, "", measurement.Value)
		assert.NotNil(t, measurement.FinishedAt)
		assert.Equal(t, v1alpha1.AnalysisPhaseError, measurement.Phase)
	})
	t.Run("with multiple results", func(t *testing.T) {
		// cover branch where results slice is longer than 1
		mock := &mockAPI{
			response: []nrdb.NRDBResult{map[string]interface{}{"key": "value"}, map[string]interface{}{"func": func() {}}},
		}
		p := NewNewRelicProvider(mock, *e)
		measurement := p.Run(newAnalysisRun(), metric)
		assert.Equal(t, "could not marshal results: json: unsupported type: func()", measurement.Message)
		assert.Equal(t, "", measurement.Value)
		assert.Equal(t, v1alpha1.AnalysisPhaseError, measurement.Phase)
	})
}

func TestResume(t *testing.T) {
	e := log.WithField("", "")
	mock := &mockAPI{}
	p := NewNewRelicProvider(mock, *e)
	metric := v1alpha1.Metric{
		Name:             "foo",
		SuccessCondition: "result == 10",
		FailureCondition: "result != 10",
		Provider: v1alpha1.MetricProvider{
			NewRelic: &v1alpha1.NewRelicMetric{
				Query: "test",
			},
		},
	}
	now := metav1.Now()
	previousMeasurement := v1alpha1.Measurement{
		StartedAt: &now,
		Phase:     v1alpha1.AnalysisPhaseInconclusive,
	}
	measurement := p.Resume(newAnalysisRun(), metric, previousMeasurement)
	assert.Equal(t, previousMeasurement, measurement)
}

func TestTerminate(t *testing.T) {
	e := log.NewEntry(log.New())
	mock := &mockAPI{}
	p := NewNewRelicProvider(mock, *e)
	metric := v1alpha1.Metric{}
	now := metav1.Now()
	previousMeasurement := v1alpha1.Measurement{
		StartedAt: &now,
		Phase:     v1alpha1.AnalysisPhaseRunning,
	}
	measurement := p.Terminate(newAnalysisRun(), metric, previousMeasurement)
	assert.Equal(t, previousMeasurement, measurement)
}

func TestGarbageCollect(t *testing.T) {
	e := log.NewEntry(log.New())
	mock := &mockAPI{}
	p := NewNewRelicProvider(mock, *e)
	err := p.GarbageCollect(nil, v1alpha1.Metric{}, 0)
	assert.NoError(t, err)
}

func TestNewNewRelicAPIClient(t *testing.T) {
	metric := v1alpha1.Metric{
		Provider: v1alpha1.MetricProvider{
			NewRelic: &v1alpha1.NewRelicMetric{},
		},
	}
	os.Unsetenv(EnvVarArgoRolloutsNewRelicAccountId)
	os.Unsetenv(EnvVarArgoRolloutsNewRelicApiKey)
	os.Unsetenv(EnvVarArgoRolloutsNewRelicBaseUrlRest)
	os.Unsetenv(EnvVarArgoRolloutsNewRelicRegion)
	os.Unsetenv(EnvVarArgoRolloutsNewRelicBaseUrlNerdGraph)
	t.Run("with default settings", func(t *testing.T) {
		os.Setenv(EnvVarArgoRolloutsNewRelicApiKey, "ABCDEFG01234")
		os.Setenv(EnvVarArgoRolloutsNewRelicAccountId, "12345")
		_, err := NewNewRelicAPIClient(metric)
		assert.Nil(t, err)
	})
	t.Run("with region specified", func(t *testing.T) {
		os.Setenv(EnvVarArgoRolloutsNewRelicApiKey, "ABCDEFG01234")
		os.Setenv(EnvVarArgoRolloutsNewRelicAccountId, "12345")
		os.Setenv(EnvVarArgoRolloutsNewRelicRegion, "eu")
		_, err := NewNewRelicAPIClient(metric)
		assert.Nil(t, err)
	})
	t.Run("when the region is invalid", func(t *testing.T) {
		os.Setenv(EnvVarArgoRolloutsNewRelicApiKey, "ABCDEFG01234")
		os.Setenv(EnvVarArgoRolloutsNewRelicAccountId, "12345")
		os.Setenv(EnvVarArgoRolloutsNewRelicRegion, "prod")
		_, err := NewNewRelicAPIClient(metric)
		// client defaults to US when not set or set to something incorrect, does not error
		assert.Nil(t, err)
	})

	t.Run("when a base-url is set", func(t *testing.T) {
		os.Setenv(EnvVarArgoRolloutsNewRelicApiKey, "ABCDEFG01234")
		os.Setenv(EnvVarArgoRolloutsNewRelicAccountId, "12345")
		os.Setenv(EnvVarArgoRolloutsNewRelicBaseUrlRest, "example.com/api/v2")
		os.Setenv(EnvVarArgoRolloutsNewRelicBaseUrlNerdGraph, "example.com/query")
		_, err := NewNewRelicAPIClient(metric)

		assert.Nil(t, err)
	})
	t.Run("with api token or account id missing missing", func(t *testing.T) {
		os.Unsetenv(EnvVarArgoRolloutsNewRelicRegion)
		os.Unsetenv(EnvVarArgoRolloutsNewRelicAccountId)
		os.Setenv(EnvVarArgoRolloutsNewRelicApiKey, "ABCDEFG01234")
		_, err := NewNewRelicAPIClient(metric)
		assert.EqualError(t, err, "account ID or personal API key not found")
	})
	t.Run("with a non-integer account ID", func(t *testing.T) {
		os.Setenv(EnvVarArgoRolloutsNewRelicApiKey, "ABCDEFG01234")
		os.Setenv(EnvVarArgoRolloutsNewRelicAccountId, "abcdef")
		_, err := NewNewRelicAPIClient(metric)
		assert.NotNil(t, err)
	})
	t.Run("when the envvar is not found", func(t *testing.T) {
		_, err := NewNewRelicAPIClient(metric)
		assert.NotNil(t, err)
	})
}
