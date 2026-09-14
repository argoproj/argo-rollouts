package cloudwatch

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	log "github.com/sirupsen/logrus"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/evaluate"
	metricutil "github.com/argoproj/argo-rollouts/utils/metric"
	timeutil "github.com/argoproj/argo-rollouts/utils/time"
)

const (
	//ProviderType indicates the provider is cloudwatch
	ProviderType = "CloudWatch"
)

type CloudWatchClientAPI interface {
	Query(interval time.Duration, query []types.MetricDataQuery) (*cloudwatch.GetMetricDataOutput, error)
}

type CloudWatchClient struct {
	client *cloudwatch.Client
}

func (c *CloudWatchClient) Query(interval time.Duration, query []types.MetricDataQuery) (*cloudwatch.GetMetricDataOutput, error) {
	endTime := timeutil.Now()
	startTime := endTime.Add(-interval)
	return c.client.GetMetricData(context.TODO(), &cloudwatch.GetMetricDataInput{
		StartTime:         &startTime,
		EndTime:           &endTime,
		MetricDataQueries: query,
	})
}

// Provider contains all the required components to run a CloudWatch query
// Implements the Provider Interface
type Provider struct {
	api    CloudWatchClientAPI
	logCtx log.Entry
}

func (p *Provider) Type() string {
	return ProviderType
}

// GetMetadata returns any additional metadata which needs to be stored & displayed as part of the metrics result.
func (p *Provider) GetMetadata(metric v1alpha1.Metric) map[string]string {
	return nil
}

// Run queries with CloudWatch provider for the metric
func (p *Provider) Run(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric) v1alpha1.Measurement {
	startTime := timeutil.MetaNow()
	measurement := v1alpha1.Measurement{
		StartedAt: &startTime,
		Metadata:  map[string]string{},
	}

	interval := time.Minute * 5
	if metric.Provider.CloudWatch.Interval != "" {
		d, err := metric.Provider.CloudWatch.Interval.Duration()
		if err != nil {
			return metricutil.MarkMeasurementError(measurement, err)
		}
		interval = d
	}

	query := convertType(metric.Provider.CloudWatch.MetricDataQueries)
	result, err := p.api.Query(interval, query)
	if err != nil {
		return metricutil.MarkMeasurementError(measurement, err)
	}

	for _, message := range result.Messages {
		p.logCtx.Warnf("CloudWatch returned the following messages: %+v", message)
	}
	if len(result.Messages) > 0 {
		measurement.Metadata["messages"] = fmt.Sprintf("%+v", result.Messages)
	}

	value := [][]float64{}
	for _, result := range result.MetricDataResults {
		value = append(value, result.Values)
	}
	measurement.Value = fmt.Sprintf("%+v", value)

	status, err := evaluate.EvaluateResult(result.MetricDataResults, metric, p.logCtx)
	if err != nil {
		return metricutil.MarkMeasurementError(measurement, err)
	}

	measurement.Phase = status
	finishedTime := timeutil.MetaNow()
	measurement.FinishedAt = &finishedTime

	return measurement
}

func (p *Provider) Resume(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, measurement v1alpha1.Measurement) v1alpha1.Measurement {
	return measurement
}

func (p *Provider) Terminate(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, measurement v1alpha1.Measurement) v1alpha1.Measurement {
	return measurement
}

func (p *Provider) GarbageCollect(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, i int) error {
	return nil
}

// NewCloudWatchProvider creates a new CloudWatch provider
func NewCloudWatchProvider(api CloudWatchClientAPI, logCtx log.Entry) *Provider {
	return &Provider{
		api:    api,
		logCtx: logCtx,
	}
}

func NewCloudWatchAPIClient(metric v1alpha1.Metric, opts ...func(*cloudwatch.Options)) (CloudWatchClientAPI, error) {
	cfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		return nil, err
	}

	return &CloudWatchClient{
		client: cloudwatch.NewFromConfig(cfg, opts...),
	}, nil
}

func convertType(data []v1alpha1.CloudWatchMetricDataQuery) []types.MetricDataQuery {
	result := make([]types.MetricDataQuery, len(data))
	for i, v := range data {
		var metricStat *types.MetricStat
		if v.MetricStat != nil {
			metricStatPeriod := int32(v.MetricStat.Period.IntValue())
			metricStat = &types.MetricStat{
				Metric: &types.Metric{
					Dimensions: nil,
					MetricName: &v.MetricStat.Metric.MetricName,
					Namespace:  v.MetricStat.Metric.Namespace,
				},
				Period: &metricStatPeriod,
				Stat:   &v.MetricStat.Stat,
				Unit:   types.StandardUnit(v.MetricStat.Unit),
			}

			if v.MetricStat.Metric.Dimensions != nil {
				metricStat.Metric.Dimensions = make([]types.Dimension, len(v.MetricStat.Metric.Dimensions))
				for j, d := range v.MetricStat.Metric.Dimensions {
					name := d.Name
					value := d.Value
					metricStat.Metric.Dimensions[j] = types.Dimension{
						Name:  &name,
						Value: &value,
					}
				}
			}
		}

		var period *int32
		if v.Period != nil {
			p := int32(v.Period.IntValue())
			period = &p
		}
		id := v.Id
		result[i] = types.MetricDataQuery{
			Id:         &id,
			Expression: v.Expression,
			Label:      v.Label,
			MetricStat: metricStat,
			Period:     period,
			ReturnData: v.ReturnData,
		}
	}

	return result
}
