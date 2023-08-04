package prometheus

import (
	"fmt"
	"math"
	"os"
	"testing"
	"time"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newScalar(f float64) model.Value {
	return &model.Scalar{
		Value:     model.SampleValue(f),
		Timestamp: model.Time(0),
	}
}

func newAnalysisRun() *v1alpha1.AnalysisRun {
	return &v1alpha1.AnalysisRun{}
}

func TestType(t *testing.T) {
	e := log.Entry{}
	mock := mockAPI{
		value: newScalar(10),
	}
	timeout := int64(5)
	metric := v1alpha1.Metric{
		Name:             "foo",
		SuccessCondition: "result == 10",
		FailureCondition: "result != 10",
		Provider: v1alpha1.MetricProvider{
			Prometheus: &v1alpha1.PrometheusMetric{
				Query:   "test",
				Timeout: &timeout,
			},
		},
	}
	p, err := NewPrometheusProvider(mock, e, metric)
	assert.NoError(t, err)
	assert.Equal(t, ProviderType, p.Type())
	assert.Equal(t, p.timeout, time.Duration(timeout*int64(time.Second)))
}

func TestRunSuccessfully(t *testing.T) {
	e := log.Entry{}
	mock := mockAPI{
		value: newScalar(10),
	}
	metric := v1alpha1.Metric{
		Name:             "foo",
		SuccessCondition: "result == 10",
		FailureCondition: "result != 10",
		Provider: v1alpha1.MetricProvider{
			Prometheus: &v1alpha1.PrometheusMetric{
				Query: "test",
			},
		},
	}
	p, err := NewPrometheusProvider(mock, e, metric)

	measurement := p.Run(newAnalysisRun(), metric)
	assert.NotNil(t, measurement.StartedAt)
	assert.NoError(t, err)
	assert.Equal(t, "10", measurement.Value)
	assert.NotNil(t, measurement.FinishedAt)
	assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, measurement.Phase)
}

func TestRunSuccessfullyWithEnv(t *testing.T) {
	e := log.Entry{}
	mock := mockAPI{
		value: newScalar(10),
	}
	address := "http://127.0.0.1:9090"
	os.Setenv(EnvVarArgoRolloutsPrometheusAddress, address)
	metric := v1alpha1.Metric{
		Name:             "foo",
		SuccessCondition: "result == 10",
		FailureCondition: "result != 10",
		Provider: v1alpha1.MetricProvider{
			Prometheus: &v1alpha1.PrometheusMetric{
				Query: "test",
			},
		},
	}
	p, err := NewPrometheusProvider(mock, e, metric)
	measurement := p.Run(newAnalysisRun(), metric)
	assert.NotNil(t, measurement.StartedAt)
	assert.NoError(t, err)
	assert.Equal(t, "10", measurement.Value)
	assert.NotNil(t, measurement.FinishedAt)
	assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, measurement.Phase)
}

func TestRunSuccessfullyWithWarning(t *testing.T) {
	e := log.NewEntry(log.New())
	mock := mockAPI{
		value:    newScalar(10),
		warnings: v1.Warnings([]string{"warning", "warning2"}),
	}
	metric := v1alpha1.Metric{
		Name:             "foo",
		SuccessCondition: "result == 10",
		FailureCondition: "result != 10",
		Provider: v1alpha1.MetricProvider{
			Prometheus: &v1alpha1.PrometheusMetric{
				Query: "test",
			},
		},
	}
	p, err := NewPrometheusProvider(mock, *e, metric)
	measurement := p.Run(newAnalysisRun(), metric)
	assert.NotNil(t, measurement.StartedAt)
	assert.NoError(t, err)
	assert.Equal(t, "10", measurement.Value)
	assert.NotNil(t, measurement.FinishedAt)
	assert.Equal(t, `"warning", "warning2"`, measurement.Metadata["warnings"])
	assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, measurement.Phase)
}

func TestRunSuccessfullyWithWarningWithEnv(t *testing.T) {
	e := log.NewEntry(log.New())
	mock := mockAPI{
		value:    newScalar(10),
		warnings: v1.Warnings([]string{"warning", "warning2"}),
	}
	metric := v1alpha1.Metric{
		Name:             "foo",
		SuccessCondition: "result == 10",
		FailureCondition: "result != 10",
		Provider: v1alpha1.MetricProvider{
			Prometheus: &v1alpha1.PrometheusMetric{
				Query: "test",
			},
		},
	}
	p, err := NewPrometheusProvider(mock, *e, metric)
	measurement := p.Run(newAnalysisRun(), metric)
	assert.NotNil(t, measurement.StartedAt)
	assert.NoError(t, err)
	assert.Equal(t, "10", measurement.Value)
	assert.NotNil(t, measurement.FinishedAt)
	assert.Equal(t, `"warning", "warning2"`, measurement.Metadata["warnings"])
	assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, measurement.Phase)
}

func TestRunWithQueryError(t *testing.T) {
	e := log.NewEntry(log.New())
	expectedErr := fmt.Errorf("bad big bug :(")
	mock := mockAPI{
		err: expectedErr,
	}
	metric := v1alpha1.Metric{
		Name:             "foo",
		SuccessCondition: "result == 10",
		FailureCondition: "result != 10",
		Provider: v1alpha1.MetricProvider{
			Prometheus: &v1alpha1.PrometheusMetric{
				Query: "test",
			},
		},
	}
	p, err := NewPrometheusProvider(mock, *e, metric)
	measurement := p.Run(newAnalysisRun(), metric)
	assert.Equal(t, expectedErr.Error(), measurement.Message)
	assert.NoError(t, err)
	assert.NotNil(t, measurement.StartedAt)
	assert.Equal(t, "", measurement.Value)
	assert.NotNil(t, measurement.FinishedAt)
	assert.Equal(t, v1alpha1.AnalysisPhaseError, measurement.Phase)
}

func TestRunWithResolveArgsError(t *testing.T) {
	e := log.Entry{}
	expectedErr := fmt.Errorf("failed to resolve {{args.var}}")
	mock := mockAPI{
		err: expectedErr,
	}
	metric := v1alpha1.Metric{
		Name: "foo",
		Provider: v1alpha1.MetricProvider{
			Prometheus: &v1alpha1.PrometheusMetric{
				Query: "test-{{args.var}}",
			},
		},
	}
	p, err := NewPrometheusProvider(mock, e, metric)
	measurement := p.Run(newAnalysisRun(), metric)
	assert.Equal(t, expectedErr.Error(), measurement.Message)
	assert.NoError(t, err)
	assert.NotNil(t, measurement.StartedAt)
	assert.Equal(t, "", measurement.Value)
	assert.NotNil(t, measurement.FinishedAt)
	assert.Equal(t, v1alpha1.AnalysisPhaseError, measurement.Phase)
}

func TestGetStatusReturnsResolvedQuery(t *testing.T) {
	e := log.Entry{}
	mock := mockAPI{}
	metric := v1alpha1.Metric{
		Name: "foo",
		Provider: v1alpha1.MetricProvider{
			Prometheus: &v1alpha1.PrometheusMetric{
				Query: "resolved-query",
			},
		},
	}
	p, err := NewPrometheusProvider(mock, e, metric)
	metricsMetadata := p.GetMetadata(metric)
	assert.NotNil(t, metricsMetadata)
	assert.NoError(t, err)
	assert.Equal(t, "resolved-query", metricsMetadata["ResolvedPrometheusQuery"])
}

func TestRunWithEvaluationError(t *testing.T) {
	e := log.WithField("", "")
	mock := mockAPI{}
	metric := v1alpha1.Metric{
		Name:             "foo",
		SuccessCondition: "result == 10",
		FailureCondition: "result != 10",
		Provider: v1alpha1.MetricProvider{
			Prometheus: &v1alpha1.PrometheusMetric{
				Query: "test",
			},
		},
	}
	p, err := NewPrometheusProvider(mock, *e, metric)
	measurement := p.Run(newAnalysisRun(), metric)
	assert.Equal(t, "Prometheus metric type not supported", measurement.Message)
	assert.NoError(t, err)
	assert.NotNil(t, measurement.StartedAt)
	assert.Equal(t, "", measurement.Value)
	assert.NotNil(t, measurement.FinishedAt)
	assert.Equal(t, v1alpha1.AnalysisPhaseError, measurement.Phase)
}

func TestResume(t *testing.T) {
	e := log.WithField("", "")
	mock := mockAPI{}
	metric := v1alpha1.Metric{
		Name:             "foo",
		SuccessCondition: "result == 10",
		FailureCondition: "result != 10",
		Provider: v1alpha1.MetricProvider{
			Prometheus: &v1alpha1.PrometheusMetric{
				Query: "test",
			},
		},
	}
	p, err := NewPrometheusProvider(mock, *e, metric)
	assert.NoError(t, err)
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
	mock := mockAPI{}
	metric := v1alpha1.Metric{}
	p, err := NewPrometheusProvider(mock, *e, metric)
	assert.NoError(t, err)
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
	mock := mockAPI{}
	metric := v1alpha1.Metric{}
	p, err := NewPrometheusProvider(mock, *e, metric)
	assert.NoError(t, err)
	err = p.GarbageCollect(nil, v1alpha1.Metric{}, 0)
	assert.NoError(t, err)
}

func TestProcessScalarResponse(t *testing.T) {
	logCtx := log.WithField("test", "test")
	p := Provider{
		logCtx: *logCtx,
	}
	metric := v1alpha1.Metric{
		SuccessCondition: "result == 10",
		FailureCondition: "result != 10",
	}

	response := &model.Scalar{
		Value:     model.SampleValue(10),
		Timestamp: model.Time(0),
	}

	value, status, err := p.processResponse(metric, response)
	assert.Nil(t, err)
	assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, status)
	assert.Equal(t, "10", value)

}

func TestProcessNaNResponse(t *testing.T) {
	logCtx := log.WithField("test", "test")
	p := Provider{
		logCtx: *logCtx,
	}

	response := &model.Scalar{
		Value:     model.SampleValue(math.NaN()),
		Timestamp: model.Time(0),
	}

	t.Run("inconclusive", func(t *testing.T) {
		metric := v1alpha1.Metric{
			SuccessCondition: "result >= 0.9",
			FailureCondition: "result < 0.9",
		}

		value, status, err := p.processResponse(metric, response)
		assert.Nil(t, err)
		assert.Equal(t, v1alpha1.AnalysisPhaseInconclusive, status)
		assert.Equal(t, "NaN", value)
	})

	t.Run("success", func(t *testing.T) {
		metric := v1alpha1.Metric{
			SuccessCondition: "result >= 0.9 || isNaN(result)",
		}

		value, status, err := p.processResponse(metric, response)
		assert.Nil(t, err)
		assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, status)
		assert.Equal(t, "NaN", value)
	})

	t.Run("failure", func(t *testing.T) {
		metric := v1alpha1.Metric{
			SuccessCondition: "result >= 0.9",
		}

		value, status, err := p.processResponse(metric, response)
		assert.Nil(t, err)
		assert.Equal(t, v1alpha1.AnalysisPhaseFailed, status)
		assert.Equal(t, "NaN", value)
	})
}

func TestProcessInfResponse(t *testing.T) {
	logCtx := log.WithField("test", "test")
	p := Provider{
		logCtx: *logCtx,
	}

	response := &model.Scalar{
		Value:     model.SampleValue(math.Inf(0)),
		Timestamp: model.Time(0),
	}

	t.Run("success", func(t *testing.T) {
		metric := v1alpha1.Metric{
			SuccessCondition: "result >= 0.9",
		}

		value, status, err := p.processResponse(metric, response)
		assert.Nil(t, err)
		assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, status)
		assert.Equal(t, "+Inf", value)
	})

	t.Run("failure", func(t *testing.T) {
		metric := v1alpha1.Metric{
			FailureCondition: "isInf(result)",
		}

		value, status, err := p.processResponse(metric, response)
		assert.Nil(t, err)
		assert.Equal(t, v1alpha1.AnalysisPhaseFailed, status)
		assert.Equal(t, "+Inf", value)
	})
}

func TestProcessVectorResponse(t *testing.T) {
	logCtx := log.WithField("test", "test")
	p := Provider{
		logCtx: *logCtx,
	}
	metric := v1alpha1.Metric{
		SuccessCondition: "10 in result",
		FailureCondition: "len(result) != 2",
	}

	response := model.Vector{
		{
			Value:     model.SampleValue(10),
			Timestamp: model.Time(0),
		},
		{
			Value:     model.SampleValue(11),
			Timestamp: model.Time(0),
		},
	}
	value, status, err := p.processResponse(metric, response)
	assert.Nil(t, err)
	assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, status)
	assert.Equal(t, "[10,11]", value)

}

func TestProcessInvalidResponse(t *testing.T) {
	logCtx := log.WithField("test", "test")
	p := Provider{
		logCtx: *logCtx,
	}
	metric := v1alpha1.Metric{
		SuccessCondition: "true",
		FailureCondition: "true",
	}

	value, status, err := p.processResponse(metric, nil)
	assert.NotNil(t, err)
	assert.Equal(t, v1alpha1.AnalysisPhaseError, status)
	assert.Equal(t, "", value)

}

func TestNewPrometheusAPI(t *testing.T) {
	os.Unsetenv(EnvVarArgoRolloutsPrometheusAddress)
	address := ":invalid::url"
	metric := v1alpha1.Metric{
		Provider: v1alpha1.MetricProvider{
			Prometheus: &v1alpha1.PrometheusMetric{
				Address: address,
			},
		},
	}
	api, err := NewPrometheusAPI(metric)
	assert.NotNil(t, err)
	log.Infof("api:%v", api)

	metric.Provider.Prometheus.Address = "https://www.example.com"
	metric.Provider.Prometheus.Insecure = true
	_, err = NewPrometheusAPI(metric)
	assert.Nil(t, err)
}

func TestNewPrometheusAPIWithEnv(t *testing.T) {
	os.Unsetenv(EnvVarArgoRolloutsPrometheusAddress)
	os.Setenv(EnvVarArgoRolloutsPrometheusAddress, ":invalid::url")
	address := ""
	metric := v1alpha1.Metric{
		Provider: v1alpha1.MetricProvider{
			Prometheus: &v1alpha1.PrometheusMetric{
				Address: address,
			},
		},
	}
	api, err := NewPrometheusAPI(metric)
	assert.NotNil(t, err)
	log.Infof("api:%v", api)

	os.Unsetenv(EnvVarArgoRolloutsPrometheusAddress)
	os.Setenv(EnvVarArgoRolloutsPrometheusAddress, "https://www.example.com")
	_, err = NewPrometheusAPI(metric)
	assert.Nil(t, err)
}

func TestNewPrometheusAddressNotConfigured(t *testing.T) {
	os.Unsetenv(EnvVarArgoRolloutsPrometheusAddress)
	os.Setenv(EnvVarArgoRolloutsPrometheusAddress, "")
	address := ""
	metric := v1alpha1.Metric{
		Provider: v1alpha1.MetricProvider{
			Prometheus: &v1alpha1.PrometheusMetric{
				Address: address,
			},
		},
	}
	api, err := NewPrometheusAPI(metric)
	assert.NotNil(t, err)
	log.Infof("api:%v", api)
}

func TestNewPrometheusNegativeTimeout(t *testing.T) {
	e := log.Entry{}
	mock := mockAPI{
		value: newScalar(10),
	}
	timeout := int64(-20)
	metric := v1alpha1.Metric{
		Name: "foo",
		Provider: v1alpha1.MetricProvider{
			Prometheus: &v1alpha1.PrometheusMetric{
				Query:   "test",
				Timeout: &timeout,
			},
		},
	}
	p, err := NewPrometheusProvider(mock, e, metric)
	assert.NotNil(t, err)
	assert.Nil(t, p)
}
