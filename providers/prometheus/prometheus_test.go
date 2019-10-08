package prometheus

import (
	"fmt"
	"testing"

	"github.com/prometheus/common/model"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

func newScalar(f float64) model.Value {
	return &model.Scalar{
		Value:     model.SampleValue(f),
		Timestamp: model.Time(0),
	}
}

func TestType(t *testing.T) {
	e := log.Entry{}
	mock := mockAPI{
		value: newScalar(10),
	}
	p := NewPrometheusProvider(mock, e)
	assert.Equal(t, ProviderType, p.Type())
}

func TestRunSuccessfully(t *testing.T) {
	e := log.Entry{}
	mock := mockAPI{
		value: newScalar(10),
	}
	p := NewPrometheusProvider(mock, e)
	metric := v1alpha1.Metric{
		Name:             "foo",
		SuccessCondition: "result == 10",
		FailureCondition: "result != 10",
		Provider: v1alpha1.AnalysisProvider{
			Prometheus: &v1alpha1.PrometheusMetric{
				Query: "test",
			},
		},
	}
	measurement, err := p.Run(nil, metric, []v1alpha1.Argument{})
	assert.Nil(t, err)
	assert.NotNil(t, measurement.StartedAt)
	assert.Equal(t, "10", measurement.Value)
	assert.NotNil(t, measurement.FinishedAt)
	assert.Equal(t, v1alpha1.AnalysisStatusSuccessful, measurement.Status)
}

func TestRunWithQueryError(t *testing.T) {
	e := log.Entry{}
	expectedErr := fmt.Errorf("bad big bug :(")
	mock := mockAPI{
		err: expectedErr,
	}
	p := NewPrometheusProvider(mock, e)
	metric := v1alpha1.Metric{
		Name:             "foo",
		SuccessCondition: "result == 10",
		FailureCondition: "result != 10",
		Provider: v1alpha1.AnalysisProvider{
			Prometheus: &v1alpha1.PrometheusMetric{
				Query: "test",
			},
		},
	}
	measurement, err := p.Run(nil, metric, []v1alpha1.Argument{})
	assert.Equal(t, expectedErr, err)
	assert.NotNil(t, measurement.StartedAt)
	assert.Equal(t, "", measurement.Value)
	assert.NotNil(t, measurement.FinishedAt)
	assert.Equal(t, v1alpha1.AnalysisStatusError, measurement.Status)
}

func TestRunWithBuildQueryError(t *testing.T) {
	e := log.Entry{}
	expectedErr := fmt.Errorf("failed to resolve {{inputs.var}}")
	mock := mockAPI{
		err: expectedErr,
	}
	p := NewPrometheusProvider(mock, e)
	metric := v1alpha1.Metric{
		Name: "foo",
		Provider: v1alpha1.AnalysisProvider{
			Prometheus: &v1alpha1.PrometheusMetric{
				Query: "test-{{inputs.var}}",
			},
		},
	}
	measurement, err := p.Run(nil, metric, []v1alpha1.Argument{})
	assert.Equal(t, expectedErr, err)
	assert.NotNil(t, measurement.StartedAt)
	assert.Equal(t, "", measurement.Value)
	assert.NotNil(t, measurement.FinishedAt)
	assert.Equal(t, v1alpha1.AnalysisStatusError, measurement.Status)
}

func TestRunWithEvaluationError(t *testing.T) {
	e := log.WithField("", "")
	mock := mockAPI{}
	p := NewPrometheusProvider(mock, *e)
	metric := v1alpha1.Metric{
		Name:             "foo",
		SuccessCondition: "result == 10",
		FailureCondition: "result != 10",
		Provider: v1alpha1.AnalysisProvider{
			Prometheus: &v1alpha1.PrometheusMetric{
				Query: "test",
			},
		},
	}
	measurement, err := p.Run(nil, metric, []v1alpha1.Argument{})
	assert.NotNil(t, err)
	assert.NotNil(t, measurement.StartedAt)
	assert.Equal(t, "", measurement.Value)
	assert.NotNil(t, measurement.FinishedAt)
	assert.Equal(t, v1alpha1.AnalysisStatusError, measurement.Status)
}

func TestResume(t *testing.T) {
	e := log.WithField("", "")
	mock := mockAPI{}
	p := NewPrometheusProvider(mock, *e)
	metric := v1alpha1.Metric{
		Name:             "foo",
		SuccessCondition: "result == 10",
		FailureCondition: "result != 10",
		Provider: v1alpha1.AnalysisProvider{
			Prometheus: &v1alpha1.PrometheusMetric{
				Query: "test",
			},
		},
	}
	now := metav1.Now()
	previousMeasurement := v1alpha1.Measurement{
		StartedAt: &now,
		Status:    v1alpha1.AnalysisStatusInconclusive,
	}
	measurement, err := p.Resume(nil, metric, []v1alpha1.Argument{}, previousMeasurement)
	assert.Nil(t, err)
	assert.Equal(t, previousMeasurement, measurement)
}

func TestTerminate(t *testing.T) {
	e := log.NewEntry(log.New())
	mock := mockAPI{}
	p := NewPrometheusProvider(mock, *e)
	metric := v1alpha1.Metric{}
	now := metav1.Now()
	previousMeasurement := v1alpha1.Measurement{
		StartedAt: &now,
		Status:    v1alpha1.AnalysisStatusRunning,
	}
	measurement, err := p.Terminate(nil, metric, []v1alpha1.Argument{}, previousMeasurement)
	assert.Nil(t, err)
	assert.Equal(t, previousMeasurement, measurement)
}

func TestEvaluateResultWithSuccess(t *testing.T) {
	p := Provider{}
	metric := v1alpha1.Metric{
		SuccessCondition: "true",
		FailureCondition: "false",
	}
	status := p.evaluateResult(true, metric)
	assert.Equal(t, v1alpha1.AnalysisStatusSuccessful, status)
}

func TestEvaluateResultWithFailure(t *testing.T) {
	p := Provider{}
	metric := v1alpha1.Metric{
		SuccessCondition: "true",
		FailureCondition: "true",
	}
	status := p.evaluateResult(true, metric)
	assert.Equal(t, v1alpha1.AnalysisStatusFailed, status)

}

func TestEvaluateResultInconclusive(t *testing.T) {
	p := Provider{}
	metric := v1alpha1.Metric{
		SuccessCondition: "false",
		FailureCondition: "false",
	}
	status := p.evaluateResult(true, metric)
	assert.Equal(t, v1alpha1.AnalysisStatusInconclusive, status)
}

func TestEvaluateResultWithErrorOnSuccessCondition(t *testing.T) {
	logCtx := log.WithField("test", "test")
	p := Provider{
		logCtx: *logCtx,
	}
	metric := v1alpha1.Metric{
		SuccessCondition: "a == true",
		FailureCondition: "true",
	}
	status := p.evaluateResult(true, metric)
	assert.Equal(t, v1alpha1.AnalysisStatusError, status)

}

func TestEvaluateResultWithErrorOnFailureCondition(t *testing.T) {
	logCtx := log.WithField("test", "test")
	p := Provider{
		logCtx: *logCtx,
	}
	metric := v1alpha1.Metric{
		SuccessCondition: "true",
		FailureCondition: "a == true",
	}
	status := p.evaluateResult(true, metric)
	assert.Equal(t, v1alpha1.AnalysisStatusError, status)

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
	assert.Equal(t, v1alpha1.AnalysisStatusSuccessful, status)
	assert.Equal(t, "10", value)

}

func TestProcessVectorResponse(t *testing.T) {
	logCtx := log.WithField("test", "test")
	p := Provider{
		logCtx: *logCtx,
	}
	metric := v1alpha1.Metric{
		SuccessCondition: "10 in result",
		FailureCondition: "false",
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
	assert.Equal(t, v1alpha1.AnalysisStatusSuccessful, status)
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
	assert.Equal(t, v1alpha1.AnalysisStatusError, status)
	assert.Equal(t, "", value)

}

func TestNewPrometheusAPI(t *testing.T) {
	metric := v1alpha1.Metric{
		Provider: v1alpha1.AnalysisProvider{
			Prometheus: &v1alpha1.PrometheusMetric{
				Server: ":invalid::url",
			},
		},
	}
	_, err := NewPrometheusAPI(metric)
	assert.NotNil(t, err)

	metric.Provider.Prometheus.Server = "https://www.example.com"
	_, err = NewPrometheusAPI(metric)
	assert.Nil(t, err)
}
