package influxdb

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	influxdb2 "github.com/influxdata/influxdb-client-go/v2/api"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	kubetesting "k8s.io/client-go/testing"
)

func newAnalysisRun() *v1alpha1.AnalysisRun {
	return &v1alpha1.AnalysisRun{}
}

func TestType(t *testing.T) {
	e := log.Entry{}
	mock := &mockAPI{}
	p := NewInfluxdbProvider(mock, e)
	assert.Equal(t, ProviderType, p.Type())
}

func TestRunSuccessfully(t *testing.T) {
	e := log.Entry{}
	csvTable := `#datatype,string,long,dateTime:RFC3339,dateTime:RFC3339,dateTime:RFC3339,double,string,string,string,string
#group,false,false,true,true,false,false,true,true,true,true
#default,_result,,,,,,,,,
,result,table,_start,_stop,_time,_value,_field,_measurement,a,b
,,0,2020-02-17T22:19:49.747562847Z,2020-02-18T22:19:49.747562847Z,2020-02-18T10:34:08.135814545Z,1.0,f,test,1,adsfasdf
,,0,2020-02-17T22:19:49.747562847Z,2020-02-18T22:19:49.747562847Z,2020-02-18T22:08:44.850214724Z,6.6,f,test,1,adsfasdf
`
	reader := strings.NewReader(csvTable)
	result := influxdb2.NewQueryTableResult(io.NopCloser(reader))
	mock := &mockAPI{response: result}
	p := NewInfluxdbProvider(mock, e)
	metric := v1alpha1.Metric{
		Name:             "foo",
		SuccessCondition: "result[0] == 1",
		FailureCondition: "result[0] != 1",
		Provider: v1alpha1.MetricProvider{
			Influxdb: &v1alpha1.InfluxdbMetric{
				Query: "test",
			},
		},
	}
	measurement := p.Run(newAnalysisRun(), metric)
	metricsMetadata := p.GetMetadata(metric)
	assert.Nil(t, metricsMetadata)
	assert.NotNil(t, measurement.StartedAt)
	assert.Equal(t, `[1 6.6]`, measurement.Value)
	assert.NotNil(t, measurement.FinishedAt)
	assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, measurement.Phase)
}

func TestRunWithTimeseries(t *testing.T) {
	e := log.Entry{}
	csvTable := `#datatype,string,long,dateTime:RFC3339,dateTime:RFC3339,dateTime:RFC3339,double,string,string,string,string
#group,false,false,true,true,false,false,true,true,true,true
#default,_result,,,,,,,,,
,result,table,_start,_stop,_time,_value,_field,_measurement,a,b
,,0,2020-02-17T22:19:49.747562847Z,2020-02-18T22:19:49.747562847Z,2020-02-18T10:34:08.135814545Z,10,f,test,1,adsfasdf
,,0,2020-02-17T22:19:49.747562847Z,2020-02-18T22:19:49.747562847Z,2020-02-18T22:08:44.850214724Z,20,f,test,1,adsfasdf
`
	reader := strings.NewReader(csvTable)
	result := influxdb2.NewQueryTableResult(io.NopCloser(reader))
	mock := &mockAPI{response: result}
	p := NewInfluxdbProvider(mock, e)
	metric := v1alpha1.Metric{
		Name:             "foo",
		SuccessCondition: "result[0] == 10",
		FailureCondition: "result[1] <  20",
		Provider: v1alpha1.MetricProvider{
			Influxdb: &v1alpha1.InfluxdbMetric{
				Query: "test",
			},
		},
	}
	measurement := p.Run(newAnalysisRun(), metric)
	metricsMetadata := p.GetMetadata(metric)
	assert.Nil(t, metricsMetadata)
	assert.NotNil(t, measurement.StartedAt)
	assert.Equal(t, `[10 20]`, measurement.Value)
	assert.NotNil(t, measurement.FinishedAt)
	assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, measurement.Phase)
}

func TestRunWithEmptyResult(t *testing.T) {
	e := log.NewEntry(log.New())
	expectedErr := fmt.Errorf("no results returned from flux query")
	csvTable := `#datatype,string,long,dateTime:RFC3339,dateTime:RFC3339,dateTime:RFC3339,double,string,string,string,string
#group,false,false,true,true,false,false,true,true,true,true
#default,_result,,,,,,,,,
,result,table,_start,_stop,_time,_value,_field,_measurement,a,b
`
	reader := strings.NewReader(csvTable)
	result := influxdb2.NewQueryTableResult(io.NopCloser(reader))
	mock := &mockAPI{response: result}
	p := NewInfluxdbProvider(mock, *e)
	metric := v1alpha1.Metric{
		Name:             "foo",
		SuccessCondition: "result[0] == 10",
		FailureCondition: "result[0] != 10",
		Provider: v1alpha1.MetricProvider{
			Influxdb: &v1alpha1.InfluxdbMetric{
				Query: "test",
			},
		},
	}
	measurement := p.Run(newAnalysisRun(), metric)
	metricsMetadata := p.GetMetadata(metric)
	assert.Nil(t, metricsMetadata)
	assert.Equal(t, expectedErr.Error(), measurement.Message)
	assert.NotNil(t, measurement.StartedAt)
	assert.Equal(t, "", measurement.Value)
	assert.NotNil(t, measurement.FinishedAt)
	assert.Equal(t, v1alpha1.AnalysisPhaseError, measurement.Phase)
}

func TestRunWithEvaluationError(t *testing.T) {
	e := log.WithField("", "")
	mock := &mockAPI{}
	p := NewInfluxdbProvider(mock, *e)
	metric := v1alpha1.Metric{
		Name:             "foo",
		SuccessCondition: "result == 10",
		FailureCondition: "result != 10",
		Provider: v1alpha1.MetricProvider{
			Influxdb: &v1alpha1.InfluxdbMetric{
				Query: "test",
			},
		},
	}
	measurement := p.Run(newAnalysisRun(), metric)
	metricsMetadata := p.GetMetadata(metric)
	assert.Nil(t, metricsMetadata)
	assert.Equal(t, "no QueryTableResult returned from flux query", measurement.Message)
	assert.NotNil(t, measurement.StartedAt)
	assert.Equal(t, "", measurement.Value)
	assert.NotNil(t, measurement.FinishedAt)
	assert.Equal(t, v1alpha1.AnalysisPhaseError, measurement.Phase)
}

func TestResume(t *testing.T) {
	e := log.WithField("", "")
	mock := &mockAPI{}
	p := NewInfluxdbProvider(mock, *e)
	metric := v1alpha1.Metric{
		Name:             "foo",
		SuccessCondition: "result == 10",
		FailureCondition: "result != 10",
		Provider: v1alpha1.MetricProvider{
			Influxdb: &v1alpha1.InfluxdbMetric{
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
	p := NewInfluxdbProvider(mock, *e)
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
	p := NewInfluxdbProvider(mock, *e)
	err := p.GarbageCollect(nil, v1alpha1.Metric{}, 0)
	assert.NoError(t, err)
}

func TestNewInfluxdbAPI(t *testing.T) {
	metric := v1alpha1.Metric{
		Provider: v1alpha1.MetricProvider{
			Influxdb: &v1alpha1.InfluxdbMetric{},
		},
	}
	tokenSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: DefaultInfluxdbTokensSecretName,
		},
	}
	fakeClient := k8sfake.NewSimpleClientset()
	fakeClient.PrependReactor("get", "*", func(action kubetesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, tokenSecret, nil
	})

	t.Run("with default settings", func(t *testing.T) {
		tokenSecret.Data = map[string][]byte{
			influxdbToken:   []byte("ABCDEFG01234"),
			influxdbOrg:     []byte("test-org"),
			influxdbAddress: []byte("http://localhost:8086"),
		}
		_, err := NewInfluxdbAPI(metric, fakeClient)
		assert.Nil(t, err)
	})

	t.Run("with authToken, org, or address missing", func(t *testing.T) {
		tokenSecret.Data = map[string][]byte{
			influxdbToken: []byte("ABCDEFG01234"),
		}
		_, err := NewInfluxdbAPI(metric, fakeClient)
		assert.EqualError(t, err, "authToken, org, or address not found")
	})

	t.Run("when secretName is specified by the metric", func(t *testing.T) {
		metric.Provider.Influxdb.Profile = "my-influx-token-secret"
		tokenSecret.Name = "my-influx-token-secret"
		tokenSecret.Data = map[string][]byte{
			influxdbToken:   []byte("ABCDEFG01234"),
			influxdbOrg:     []byte("test-org"),
			influxdbAddress: []byte("http://localhost:8086"),
		}
		_, err := NewInfluxdbAPI(metric, fakeClient)
		assert.Nil(t, err)
	})
	t.Run("when the secret is not found", func(t *testing.T) {
		fakeClient.PrependReactor("get", "*", func(action kubetesting.Action) (handled bool, ret runtime.Object, err error) {
			return true, nil, errors.New("secret not found")
		})
		_, err := NewInfluxdbAPI(metric, fakeClient)
		assert.NotNil(t, err)
	})
}
