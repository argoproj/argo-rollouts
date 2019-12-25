package wavefront

import (
	"fmt"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	log "github.com/sirupsen/logrus"
	wavefront_api "github.com/spaceapegames/go-wavefront"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	kubetesting "k8s.io/client-go/testing"
	"math"
	"testing"
)


func newAnalysisRun() *v1alpha1.AnalysisRun {
	return &v1alpha1.AnalysisRun{}
}

func TestRunSuccessfully(t *testing.T) {
	e := log.Entry{}
	mockSeries := wavefront_api.TimeSeries{
		DataPoints: []wavefront_api.DataPoint{
			[]float64{12000,10},
		},
	}
	mock := mockAPI{
		response: &wavefront_api.QueryResponse{
			TimeSeries: []wavefront_api.TimeSeries{mockSeries},
		},}

	p := NewWavefrontProvider(&mock, e)
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
	measurement := p.Run(newAnalysisRun(), metric)
	assert.Equal(t, expectedErr.Error(), measurement.Message)
	assert.NotNil(t, measurement.StartedAt)
	assert.Equal(t, "", measurement.Value)
	assert.NotNil(t, measurement.FinishedAt)
	assert.Equal(t, v1alpha1.AnalysisPhaseError, measurement.Phase)
}

func TestProcessNaNResponse(t *testing.T) {
	logCtx := log.WithField("test", "test")
	p := Provider{
		logCtx: *logCtx,
	}
	metric := v1alpha1.Metric{
		SuccessCondition: "true",
		FailureCondition: "false",
	}

	mockSeries := wavefront_api.TimeSeries{
		DataPoints: []wavefront_api.DataPoint{
			[]float64{12000, math.NaN()},
		},
	}

	response := &wavefront_api.QueryResponse{
		TimeSeries: []wavefront_api.TimeSeries{mockSeries},
	}

	value, status, err := p.processResponse(metric, response)
	assert.Nil(t, err)
	assert.Equal(t, v1alpha1.AnalysisPhaseInconclusive, status)
	assert.Equal(t, "NaN", value)

}

func TestProcessMutipleTimeseriesResponse(t *testing.T) {
	logCtx := log.WithField("test", "test")
	p := Provider{
		logCtx: *logCtx,
	}
	metric := v1alpha1.Metric{
		SuccessCondition: "10 in result",
		FailureCondition: "len(result) != 2",
	}

	mockSeries1 := wavefront_api.TimeSeries{
		DataPoints: []wavefront_api.DataPoint{
			[]float64{12000,10},
		},
	}
	mockSeries2 := wavefront_api.TimeSeries{
		DataPoints: []wavefront_api.DataPoint{
			[]float64{12000,11},
		},
	}
	response := &wavefront_api.QueryResponse{
		TimeSeries: []wavefront_api.TimeSeries{mockSeries1,mockSeries2},
	}
	value, status, err := p.processResponse(metric, response)
	assert.Nil(t, err)
	assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, status)
	assert.Equal(t, "[10.00,11.00]", value)

}

func TestNewWavefrontAPI(t *testing.T) {
	metric := v1alpha1.Metric{
		Provider: v1alpha1.MetricProvider{
			Wavefront: &v1alpha1.WavefrontMetric{
				Address: "invalid url",
			},
		},
	}
	tokenSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: WavefrontTokensSecretName,
		},
		Data: map[string][]byte{
			"example.wavefront.com": []byte("123456789"),
		},
	}
	fakeClient := k8sfake.NewSimpleClientset()
	fakeClient.PrependReactor("get", "*", func(action kubetesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, tokenSecret, nil
	})

	_, err := NewWavefrontAPI(metric, fakeClient)
	assert.NotNil(t, err)
	assert.Equal(t, err.Error(), "API token not found")

	metric.Provider.Wavefront.Address = "example.wavefront.com"
	_, err = NewWavefrontAPI(metric, fakeClient)
	assert.Nil(t, err)
}
