package wavefront

import (
	"fmt"
	"os"
	"testing"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	log "github.com/sirupsen/logrus"
	wavefrontapi "github.com/spaceapegames/go-wavefront"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newAnalysisRun() *v1alpha1.AnalysisRun {
	return &v1alpha1.AnalysisRun{}
}

func TestType(t *testing.T) {
	e := log.Entry{}
	mockSeries := wavefrontapi.TimeSeries{
		DataPoints: []wavefrontapi.DataPoint{
			[]float64{12000, 10},
		},
	}
	mock := mockAPI{
		response: &wavefrontapi.QueryResponse{
			TimeSeries: []wavefrontapi.TimeSeries{mockSeries},
		}}
	p := NewWavefrontProvider(mock, e)
	assert.Equal(t, ProviderType, p.Type())
}

func TestRunSuccessfully(t *testing.T) {
	e := log.WithField("", "")
	mockSeries := wavefrontapi.TimeSeries{
		DataPoints: []wavefrontapi.DataPoint{
			[]float64{12000, 10},
		},
	}
	mock := mockAPI{
		response: &wavefrontapi.QueryResponse{
			TimeSeries: []wavefrontapi.TimeSeries{mockSeries},
		}}

	p := NewWavefrontProvider(&mock, *e)
	metric := v1alpha1.Metric{
		Name:             "foo",
		SuccessCondition: "result == 10",
		FailureCondition: "result != 10",
		Provider: v1alpha1.MetricProvider{
			Wavefront: &v1alpha1.WavefrontMetric{
				Query: "test",
			},
		},
	}
	metricsMetadata := p.GetMetadata(metric)
	assert.Nil(t, metricsMetadata)

	measurement := p.Run(newAnalysisRun(), metric)
	assert.NotNil(t, measurement.StartedAt)
	assert.Equal(t, "10.00", measurement.Value)
	assert.NotNil(t, measurement.FinishedAt)
	assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, measurement.Phase)
}

func TestRunWithQueryError(t *testing.T) {
	e := log.Entry{}
	expectedErr := fmt.Errorf("bad big bug :(")
	mock := mockAPI{
		err: expectedErr,
	}
	p := NewWavefrontProvider(mock, e)
	metric := v1alpha1.Metric{
		Name:             "foo",
		SuccessCondition: "result == 10",
		FailureCondition: "result != 10",
		Provider: v1alpha1.MetricProvider{
			Wavefront: &v1alpha1.WavefrontMetric{
				Query: "test",
			},
		},
	}
	metricsMetadata := p.GetMetadata(metric)
	assert.Nil(t, metricsMetadata)

	measurement := p.Run(newAnalysisRun(), metric)
	assert.Equal(t, expectedErr.Error(), measurement.Message)
	assert.NotNil(t, measurement.StartedAt)
	assert.Equal(t, "", measurement.Value)
	assert.NotNil(t, measurement.FinishedAt)
	assert.Equal(t, v1alpha1.AnalysisPhaseError, measurement.Phase)
}

func TestRunWithEvaluationError(t *testing.T) {
	e := log.WithField("", "")
	mock := mockAPI{
		response: &wavefrontapi.QueryResponse{
			TimeSeries: []wavefrontapi.TimeSeries{},
			Warnings:   "No query provided",
		}}
	p := NewWavefrontProvider(mock, *e)
	metric := v1alpha1.Metric{
		Name:             "foo",
		SuccessCondition: "result == 10",
		FailureCondition: "result != 10",
		Provider: v1alpha1.MetricProvider{
			Wavefront: &v1alpha1.WavefrontMetric{
				Query: "test",
			},
		},
	}
	metricsMetadata := p.GetMetadata(metric)
	assert.Nil(t, metricsMetadata)

	measurement := p.Run(newAnalysisRun(), metric)
	assert.Equal(t, "No TimeSeries found in response from Wavefront", measurement.Message)
	assert.Equal(t, "No query provided", measurement.Metadata["warnings"])
	assert.NotNil(t, measurement.StartedAt)
	assert.Equal(t, "", measurement.Value)
	assert.NotNil(t, measurement.FinishedAt)
	assert.Equal(t, v1alpha1.AnalysisPhaseError, measurement.Phase)
}

func TestResume(t *testing.T) {
	e := log.WithField("", "")
	mock := mockAPI{}
	p := NewWavefrontProvider(mock, *e)
	metric := v1alpha1.Metric{
		Name:             "foo",
		SuccessCondition: "result == 10",
		FailureCondition: "result != 10",
		Provider: v1alpha1.MetricProvider{
			Wavefront: &v1alpha1.WavefrontMetric{
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
	mock := mockAPI{}
	p := NewWavefrontProvider(mock, *e)
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
	mock := mockAPI{}
	p := NewWavefrontProvider(mock, *e)
	err := p.GarbageCollect(nil, v1alpha1.Metric{}, 0)
	assert.NoError(t, err)
}

func TestProcessMultipleTimeseriesResponse(t *testing.T) {
	logCtx := log.WithField("test", "test")
	p := Provider{
		logCtx: *logCtx,
	}
	metric := v1alpha1.Metric{
		SuccessCondition: "10 in result",
		FailureCondition: "len(result) != 2",
	}

	mockSeries1 := wavefrontapi.TimeSeries{
		DataPoints: []wavefrontapi.DataPoint{
			[]float64{12000, 10},
		},
	}
	mockSeries2 := wavefrontapi.TimeSeries{
		DataPoints: []wavefrontapi.DataPoint{
			[]float64{12000, 11},
		},
	}
	response := &wavefrontapi.QueryResponse{
		TimeSeries: []wavefrontapi.TimeSeries{mockSeries1, mockSeries2},
	}
	result, err := p.processResponse(metric, response, metav1.Unix(12000, 0))
	assert.Nil(t, err)
	assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, result.newStatus)
	assert.Equal(t, "[10.00,11.00]", result.newValue)

}

func TestNewWavefrontAPI(t *testing.T) {
	os.Unsetenv(EnvVarArgoRolloutsWavefrontAddress)
	os.Unsetenv(EnvVarArgoRolloutsWavefrontToken)
	//os.Setenv(EnvVarArgoRolloutsWavefrontAddress, ":invalid::url")
	address := "invalid url"
	metric := v1alpha1.Metric{
		Provider: v1alpha1.MetricProvider{
			Wavefront: &v1alpha1.WavefrontMetric{
				Address: address,
			},
		},
	}
	_, err := NewWavefrontAPI(metric)
	assert.NotNil(t, err)
	assert.Equal(t, err.Error(), "parse \"https://invalid url/api/v2/\": invalid character \" \" in host name")

	metric.Provider.Wavefront.Address = "example.wavefront.com"
	_, err = NewWavefrontAPI(metric)
	assert.Nil(t, err)
}

func TestNewWavefrontAPIWithEnv(t *testing.T) {
	os.Unsetenv(EnvVarArgoRolloutsWavefrontAddress)
	os.Unsetenv(EnvVarArgoRolloutsWavefrontToken)
	os.Setenv(EnvVarArgoRolloutsWavefrontAddress, ":invalid::url")
	address := ""
	metric := v1alpha1.Metric{
		Provider: v1alpha1.MetricProvider{
			Wavefront: &v1alpha1.WavefrontMetric{
				Address: address,
			},
		},
	}
	api, err := NewWavefrontAPI(metric)
	assert.NotNil(t, err)
	log.Infof("api:%v", api)

	os.Unsetenv(EnvVarArgoRolloutsWavefrontToken)
	os.Unsetenv(EnvVarArgoRolloutsWavefrontAddress)
	os.Setenv(EnvVarArgoRolloutsWavefrontAddress, "example.wavefront.com")
	metric = v1alpha1.Metric{
		Provider: v1alpha1.MetricProvider{
			Wavefront: &v1alpha1.WavefrontMetric{
				Address: address,
			},
		},
	}
	//os.Setenv(EnvVarArgoRolloutsWavefrontToken, "")
	_, err = NewWavefrontAPI(metric)
	assert.Nil(t, err)
}

func TestFindDataPointValue(t *testing.T) {
	e := log.WithField("", "")
	mock := mockAPI{}
	p := NewWavefrontProvider(mock, *e)
	dp := func(time, value float64) []float64 {
		return []float64{time, value}
	}
	t.Run("Choose earlier but closer point", func(t *testing.T) {
		dataPoints := []wavefrontapi.DataPoint{
			dp(0, 1),
			dp(5, 2),
		}
		value, epoch, drift := p.findDataPointValue(dataPoints, metav1.Unix(1, 0))
		assert.Equal(t, float64(1), value)
		assert.Equal(t, int64(0), epoch)
		assert.Equal(t, int64(-1), drift)
	})

	t.Run("Choose later but closer point", func(t *testing.T) {
		dataPoints := []wavefrontapi.DataPoint{
			dp(0, 1),
			dp(5, 2),
		}
		value, epoch, drift := p.findDataPointValue(dataPoints, metav1.Unix(4, 0))
		assert.Equal(t, float64(2), value)
		assert.Equal(t, int64(5), epoch)
		assert.Equal(t, int64(1), drift)
	})

	t.Run("Choose exact point", func(t *testing.T) {
		dataPoints := []wavefrontapi.DataPoint{
			dp(0, 1),
			dp(5, 2),
		}
		value, epoch, drift := p.findDataPointValue(dataPoints, metav1.Unix(0, 0))
		assert.Equal(t, float64(1), value)
		assert.Equal(t, int64(0), epoch)
		assert.Equal(t, int64(0), drift)
	})
}
