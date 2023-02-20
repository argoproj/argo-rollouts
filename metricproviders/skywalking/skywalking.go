package skywalking

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/machinebox/graphql"
	log "github.com/sirupsen/logrus"
	"k8s.io/client-go/kubernetes"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/evaluate"
	metricutil "github.com/argoproj/argo-rollouts/utils/metric"
	timeutil "github.com/argoproj/argo-rollouts/utils/time"
)

type duration struct {
	Start string `json:"start"`
	End   string `json:"end"`
	Step  string `json:"step"`
}

const (
	// ProviderType indicates the provider is skywalking
	ProviderType        = "skywalking"
	defaultQueryTimeout = 30 * time.Second
)

type SkyWalkingClientAPI interface {
	Query(query string) (interface{}, error)
}

type SkyWalkingClient struct {
	*graphql.Client
	Interval time.Duration
}

// Query executes a GraphQL query against the given SkyWalking backend
func (n SkyWalkingClient) Query(query string) (interface{}, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultQueryTimeout)
	defer cancel()

	req := graphql.NewRequest(query)
	req.Var("duration", duration{
		Start: time.Now().Add(-n.Interval).Format("2006-01-02 1504"),
		End:   time.Now().Format("2006-01-02 1504"),
		Step:  "MINUTE",
	})
	var results interface{}
	err := n.Run(ctx, req, &results)
	return results, err
}

type Provider struct {
	client SkyWalkingClientAPI
	logCtx log.Entry
}

// Run queries SkyWalking for the metric
func (p *Provider) Run(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric) v1alpha1.Measurement {
	startTime := timeutil.MetaNow()
	newMeasurement := v1alpha1.Measurement{
		StartedAt: &startTime,
	}

	results, err := p.client.Query(metric.Provider.SkyWalking.Query)
	if err != nil {
		return metricutil.MarkMeasurementError(newMeasurement, err)
	}

	valueStr, newStatus, err := p.processResponse(metric, results)
	if err != nil {
		return metricutil.MarkMeasurementError(newMeasurement, err)
	}
	newMeasurement.Value = valueStr
	newMeasurement.Phase = newStatus

	finishedTime := timeutil.MetaNow()
	newMeasurement.FinishedAt = &finishedTime
	return newMeasurement
}

func toJSONString(v interface{}) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (p *Provider) processResponse(metric v1alpha1.Metric, result interface{}) (string, v1alpha1.AnalysisPhase, error) {
	if result == nil {
		return "", v1alpha1.AnalysisPhaseFailed, fmt.Errorf("no results returned from SkyWalking query")
	}
	valueStr, err := toJSONString(result)
	if err != nil {
		return "", v1alpha1.AnalysisPhaseError, fmt.Errorf("could not marshal results: %w", err)
	}
	newStatus, err := evaluate.EvaluateResult(result, metric, p.logCtx)
	return valueStr, newStatus, err
}

// Resume should not be used the SkyWalking provider since all the work should occur in the Run method
func (p *Provider) Resume(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, measurement v1alpha1.Measurement) v1alpha1.Measurement {
	p.logCtx.Warn("SkyWalking provider should not execute the Resume method")
	return measurement
}

// Terminate should not be used the SkyWalking provider since all the work should occur in the Run method
func (p *Provider) Terminate(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, measurement v1alpha1.Measurement) v1alpha1.Measurement {
	p.logCtx.Warn("SkyWalking provider should not execute the Terminate method")
	return measurement
}

// GarbageCollect should not be used the SkyWalking provider since all the work should occur in the Run method
func (p *Provider) GarbageCollect(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, i int) error {
	return nil
}

func (p *Provider) Type() string {
	return ProviderType
}

// GetMetadata returns any additional metadata which needs to be stored & displayed as part of the metrics result.
func (p *Provider) GetMetadata(metric v1alpha1.Metric) map[string]string {
	return nil
}

// NewSkyWalkingProvider creates a new SkyWalking provider
func NewSkyWalkingProvider(client SkyWalkingClientAPI, logCtx log.Entry) *Provider {
	return &Provider{
		logCtx: logCtx,
		client: client,
	}
}

// NewSkyWalkingClient creates a new GraphQL API client from metric configuration
func NewSkyWalkingClient(metric v1alpha1.Metric, kubeclientset kubernetes.Interface) (*SkyWalkingClient, error) {
	c := graphql.NewClient(metric.Provider.SkyWalking.Address + "/graphql")
	d, err := metric.Provider.SkyWalking.Interval.Duration()
	if err != nil {
		return nil, err
	}
	return &SkyWalkingClient{
		Client:   c,
		Interval: d,
	}, nil
}
