package cloudwatch

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

func newAnalysisRun() *v1alpha1.AnalysisRun {
	return &v1alpha1.AnalysisRun{}
}

func TestType(t *testing.T) {
	e := log.Entry{}
	mock := &mockAPI{}
	p := NewCloudWatchProvider(mock, e)
	assert.Equal(t, ProviderType, p.Type())
}

func TestRunSuccessfully(t *testing.T) {
	e := log.Entry{}
	mock := &mockAPI{
		response: &cloudwatch.GetMetricDataOutput{
			MetricDataResults: []types.MetricDataResult{
				{
					Values: []float64{
						0.1,
						0.2,
						0.3,
					},
				},
			},
		},
	}
	p := NewCloudWatchProvider(mock, e)
	metric := v1alpha1.Metric{
		Name:             "foo",
		SuccessCondition: "result[0].Values[0] == 0.1",
		FailureCondition: "result[0].Values[0] != 0.1",
		Provider: v1alpha1.MetricProvider{
			CloudWatch: &v1alpha1.CloudWatchMetric{
				MetricDataQueries: []v1alpha1.CloudWatchMetricDataQuery{},
			},
		},
	}
	measurement := p.Run(newAnalysisRun(), metric)
	assert.NotNil(t, measurement.StartedAt)
	assert.Equal(t, `[[0.1 0.2 0.3]]`, measurement.Value)
	assert.NotNil(t, measurement.FinishedAt)
	assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, measurement.Phase)
}

func TestRunWithQueryError(t *testing.T) {
	e := log.NewEntry(log.New())
	expectedErr := fmt.Errorf("bad big bug :(")
	mock := &mockAPI{
		err: expectedErr,
	}
	p := NewCloudWatchProvider(mock, *e)
	metric := v1alpha1.Metric{
		Name:             "foo",
		SuccessCondition: "result[0].Values[0] == 0.1",
		FailureCondition: "result[0].Values[0] != 0.1",
		Provider: v1alpha1.MetricProvider{
			CloudWatch: &v1alpha1.CloudWatchMetric{
				MetricDataQueries: []v1alpha1.CloudWatchMetricDataQuery{},
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

func TestRunWithResolveArgsError(t *testing.T) {
	e := log.NewEntry(log.New())
	expectedErr := fmt.Errorf("failed to resolve {{args.var}}")
	mock := &mockAPI{
		err: expectedErr,
	}
	p := NewCloudWatchProvider(mock, *e)
	metric := v1alpha1.Metric{
		Name:             "foo",
		SuccessCondition: "result[0].Values[0] == 0.1",
		FailureCondition: "result[0].Values[0] != 0.1",
		Provider: v1alpha1.MetricProvider{
			CloudWatch: &v1alpha1.CloudWatchMetric{
				MetricDataQueries: []v1alpha1.CloudWatchMetricDataQuery{},
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
	expectedErr := fmt.Errorf(`'operation error CloudWatch: GetMetricData, https response error StatusCode:
400, RequestID: 030a8061-95f5-4a39-8ef8-fd3dc2da8a82, api error ValidationError:
1 validation error detected: Value null at ''metricDataQueries'' failed to satisfy
constraint: Member must not be null'`)
	mock := &mockAPI{
		err: expectedErr,
	}
	p := NewCloudWatchProvider(mock, *e)
	metric := v1alpha1.Metric{
		Name:             "foo",
		SuccessCondition: "result[0].Values[0] == 0.1",
		FailureCondition: "result[0].Values[0] != 0.1",
		Provider: v1alpha1.MetricProvider{
			CloudWatch: &v1alpha1.CloudWatchMetric{
				MetricDataQueries: []v1alpha1.CloudWatchMetricDataQuery{},
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

func TestResume(t *testing.T) {
	e := log.WithField("", "")
	mock := &mockAPI{}
	p := NewCloudWatchProvider(mock, *e)
	metric := v1alpha1.Metric{
		Name:             "foo",
		SuccessCondition: "result[0].Values[0] == 0.1",
		FailureCondition: "result[0].Values[0] != 0.1",
		Provider: v1alpha1.MetricProvider{
			CloudWatch: &v1alpha1.CloudWatchMetric{
				MetricDataQueries: []v1alpha1.CloudWatchMetricDataQuery{},
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
	p := NewCloudWatchProvider(mock, *e)
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
	p := NewCloudWatchProvider(mock, *e)
	err := p.GarbageCollect(nil, v1alpha1.Metric{}, 0)
	assert.NoError(t, err)
}

func TestNewCloudWatchAPIClient(t *testing.T) {
	metric := v1alpha1.Metric{
		Provider: v1alpha1.MetricProvider{
			CloudWatch: &v1alpha1.CloudWatchMetric{},
		},
	}

	t.Run("with env settings", func(t *testing.T) {
		envs := map[string]string{
			"AWS_ACCESS_KEY_ID":     "hoge",
			"AWS_SECRET_ACCESS_KEY": "fuga",
		}
		for k, v := range envs {
			before, ok := os.LookupEnv(k)
			if err := os.Setenv(k, v); err != nil {
				t.Fatalf("os.Setenv error: %+v", err)
			}
			t.Cleanup(func() {
				if ok {
					os.Setenv(k, before)
				} else {
					os.Unsetenv(k)
				}
			})
		}
		checkEnvs := func(opt *cloudwatch.Options) {
			cred, err := opt.Credentials.Retrieve(context.TODO())
			if err != nil {
				t.Fatalf("Credentials.Retrieve error: %+v", err)
			}
			assert.Equal(t, "hoge", cred.AccessKeyID)
			assert.Equal(t, "fuga", cred.SecretAccessKey)
		}

		_, err := NewCloudWatchAPIClient(metric, checkEnvs)
		assert.Nil(t, err)
	})
}

func TestConvertType(t *testing.T) {
	period := intstr.FromInt(300)
	tests := []struct {
		query    []v1alpha1.CloudWatchMetricDataQuery
		expected []types.MetricDataQuery
	}{
		{
			query: []v1alpha1.CloudWatchMetricDataQuery{
				{
					Id:         "rate",
					Expression: ptr.To[string]("errors / requests"),
				},
				{
					Id: "errors",
					MetricStat: &v1alpha1.CloudWatchMetricStat{
						Metric: v1alpha1.CloudWatchMetricStatMetric{
							Namespace:  ptr.To[string]("app"),
							MetricName: "errors",
						},
						Period: period,
						Stat:   "Sum",
						Unit:   "Count",
					},
					ReturnData: ptr.To[bool](false),
				},
				{
					Id: "requests",
					MetricStat: &v1alpha1.CloudWatchMetricStat{
						Metric: v1alpha1.CloudWatchMetricStatMetric{
							Namespace:  ptr.To[string]("app"),
							MetricName: "requests",
						},
						Period: period,
						Stat:   "Sum",
						Unit:   "Count",
					},
					ReturnData: ptr.To[bool](false),
				},
			},
			expected: []types.MetricDataQuery{
				{
					Id:         ptr.To[string]("rate"),
					Expression: ptr.To[string]("errors / requests"),
				},
				{
					Id: ptr.To[string]("errors"),
					MetricStat: &types.MetricStat{
						Metric: &types.Metric{
							Namespace:  ptr.To[string]("app"),
							MetricName: ptr.To[string]("errors"),
						},
						Period: ptr.To[int32](300),
						Stat:   ptr.To[string]("Sum"),
						Unit:   types.StandardUnitCount,
					},
					ReturnData: ptr.To[bool](false),
				},
				{
					Id: ptr.To[string]("requests"),
					MetricStat: &types.MetricStat{
						Metric: &types.Metric{
							Namespace:  ptr.To[string]("app"),
							MetricName: ptr.To[string]("requests"),
						},
						Period: ptr.To[int32](300),
						Stat:   ptr.To[string]("Sum"),
						Unit:   types.StandardUnitCount,
					},
					ReturnData: ptr.To[bool](false),
				},
			},
		},
		{
			query: []v1alpha1.CloudWatchMetricDataQuery{
				{
					Id:         "rate",
					Expression: ptr.To[string]("errors / requests"),
				},
				{
					Id: "errors",
					MetricStat: &v1alpha1.CloudWatchMetricStat{
						Metric: v1alpha1.CloudWatchMetricStatMetric{
							Dimensions: []v1alpha1.CloudWatchMetricStatMetricDimension{
								{
									Name:  "hoge",
									Value: "fuga",
								},
							},
							Namespace:  ptr.To[string]("app1"),
							MetricName: "errors",
						},
						Period: period,
						Stat:   "Max",
						Unit:   "Count",
					},
					ReturnData: ptr.To[bool](false),
				},
				{
					Id: "requests",
					MetricStat: &v1alpha1.CloudWatchMetricStat{
						Metric: v1alpha1.CloudWatchMetricStatMetric{
							Namespace:  ptr.To[string]("app2"),
							MetricName: "requests",
						},
						Period: period,
						Stat:   "Sum",
						Unit:   "Bytes/Second",
					},
					ReturnData: ptr.To[bool](true),
				},
			},
			expected: []types.MetricDataQuery{
				{
					Id:         ptr.To[string]("rate"),
					Expression: ptr.To[string]("errors / requests"),
				},
				{
					Id: ptr.To[string]("errors"),
					MetricStat: &types.MetricStat{
						Metric: &types.Metric{
							Namespace:  ptr.To[string]("app1"),
							MetricName: ptr.To[string]("errors"),
							Dimensions: []types.Dimension{
								{
									Name:  ptr.To[string]("hoge"),
									Value: ptr.To[string]("fuga"),
								},
							},
						},
						Period: ptr.To[int32](300),
						Stat:   ptr.To[string]("Max"),
						Unit:   types.StandardUnitCount,
					},
					ReturnData: ptr.To[bool](false),
				},
				{
					Id: ptr.To[string]("requests"),
					MetricStat: &types.MetricStat{
						Metric: &types.Metric{
							Namespace:  ptr.To[string]("app2"),
							MetricName: ptr.To[string]("requests"),
						},
						Period: ptr.To[int32](300),
						Stat:   ptr.To[string]("Sum"),
						Unit:   types.StandardUnitBytesSecond,
					},
					ReturnData: ptr.To[bool](true),
				},
			},
		},
		{
			query: []v1alpha1.CloudWatchMetricDataQuery{
				{
					Id:         "rate",
					Expression: ptr.To[string]("errors / requests"),
				},
				{
					Id: "errors",
					MetricStat: &v1alpha1.CloudWatchMetricStat{
						Metric: v1alpha1.CloudWatchMetricStatMetric{
							Dimensions: []v1alpha1.CloudWatchMetricStatMetricDimension{
								{
									Name:  "hoge",
									Value: "fuga",
								},
								{
									Name:  "poge",
									Value: "doge",
								},
							},
							Namespace:  ptr.To[string]("app1"),
							MetricName: "errors",
						},
						Period: period,
						Stat:   "Max",
						Unit:   "Count",
					},
					ReturnData: ptr.To[bool](false),
				},
				{
					Id: "requests",
					MetricStat: &v1alpha1.CloudWatchMetricStat{
						Metric: v1alpha1.CloudWatchMetricStatMetric{
							Namespace:  ptr.To[string]("app2"),
							MetricName: "requests",
						},
						Period: period,
						Stat:   "Sum",
						Unit:   "Bytes/Second",
					},
					ReturnData: ptr.To[bool](true),
				},
			},
			expected: []types.MetricDataQuery{
				{
					Id:         ptr.To[string]("rate"),
					Expression: ptr.To[string]("errors / requests"),
				},
				{
					Id: ptr.To[string]("errors"),
					MetricStat: &types.MetricStat{
						Metric: &types.Metric{
							Namespace:  ptr.To[string]("app1"),
							MetricName: ptr.To[string]("errors"),
							Dimensions: []types.Dimension{
								{
									Name:  ptr.To[string]("hoge"),
									Value: ptr.To[string]("fuga"),
								},
								{
									Name:  ptr.To[string]("poge"),
									Value: ptr.To[string]("doge"),
								},
							},
						},
						Period: ptr.To[int32](300),
						Stat:   ptr.To[string]("Max"),
						Unit:   types.StandardUnitCount,
					},
					ReturnData: ptr.To[bool](false),
				},
				{
					Id: ptr.To[string]("requests"),
					MetricStat: &types.MetricStat{
						Metric: &types.Metric{
							Namespace:  ptr.To[string]("app2"),
							MetricName: ptr.To[string]("requests"),
						},
						Period: ptr.To[int32](300),
						Stat:   ptr.To[string]("Sum"),
						Unit:   types.StandardUnitBytesSecond,
					},
					ReturnData: ptr.To[bool](true),
				},
			},
		},
	}

	for _, tt := range tests {
		result := convertType(tt.query)
		assert.Equal(t, tt.expected, result)
	}
}
