package cloudwatch

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/evaluate"
	metricutil "github.com/argoproj/argo-rollouts/utils/metric"
)

const (
	//ProviderType indicates the provider is cloudwatch
	ProviderType         = "CloudWatch"
	CloudWatchSecretName = "cloudWatch"
)

type CloudWatchClientAPI interface {
	Query(interval time.Duration, query []types.MetricDataQuery) (*cloudwatch.GetMetricDataOutput, error)
}

type CloudWatchClient struct {
	client *cloudwatch.Client
}

func (c *CloudWatchClient) Query(interval time.Duration, query []types.MetricDataQuery) (*cloudwatch.GetMetricDataOutput, error) {
	endTime := time.Now()
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

// Run queries with CloudWatch provider for the metric
func (p *Provider) Run(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric) v1alpha1.Measurement {
	startTime := metav1.Now()
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

	query := []types.MetricDataQuery{}
	if err := json.Unmarshal([]byte(metric.Provider.CloudWatch.MetricDataQueries), &query); err != nil {
		return metricutil.MarkMeasurementError(measurement, err)
	}

	result, err := p.api.Query(interval, query)
	if err != nil {
		return metricutil.MarkMeasurementError(measurement, err)
	}

	for _, message := range result.Messages {
		p.logCtx.Warnf("CloudWatch returned the following messages: %+v", message)
	}

	status, err := evaluate.EvaluateResult(result.MetricDataResults, metric, p.logCtx)
	if err != nil {
		return metricutil.MarkMeasurementError(measurement, err)
	}

	value := [][]float64{}
	for _, result := range result.MetricDataResults {
		value = append(value, result.Values)
	}
	measurement.Value = fmt.Sprintf("%+v", value)
	measurement.Phase = status
	finishedTime := metav1.Now()
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

//NewCloudWatchProvider creates a new CloudWatch provider
func NewCloudWatchProvider(api CloudWatchClientAPI, logCtx log.Entry) *Provider {
	return &Provider{
		api:    api,
		logCtx: logCtx,
	}
}

func NewCloudWatchAPIClient(metric v1alpha1.Metric, kubeclientset kubernetes.Interface) (CloudWatchClientAPI, error) {
	cfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		return nil, err
	}

	return &CloudWatchClient{
		client: cloudwatch.NewFromConfig(cfg),
	}, nil
}
