package graphite

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/evaluate"
	metricutil "github.com/argoproj/argo-rollouts/utils/metric"
	timeutil "github.com/argoproj/argo-rollouts/utils/time"
)

const (
	// ProviderType indicates the provider is Graphite.
	ProviderType = "Graphite"
)

// NewAPIClient generates a APIClient from the metric configuration
func NewAPIClient(metric v1alpha1.Metric, logCTX log.Entry) (*APIClient, error) {
	addr := metric.Provider.Graphite.Address
	graphiteURL, err := url.Parse(addr)
	if addr == "" || err != nil {
		return nil, fmt.Errorf("%s address %s is not a valid URL", ProviderType, addr)
	}

	return &APIClient{
		logCTX: logCTX,
		client: &http.Client{
			Timeout: time.Duration(10) * time.Second,
		},
		url: *graphiteURL,
	}, nil
}

// Provider contains the required components to run a Graphite query.
// TODO: add support for username/password authentication.
type Provider struct {
	api    API
	logCtx log.Entry
}

// Type indicates provider is a Graphite provider.
func (p *Provider) Type() string {
	return ProviderType
}

// GetMetadata returns any additional metadata which needs to be stored & displayed as part of the metrics result.
func (p *Provider) GetMetadata(metric v1alpha1.Metric) map[string]string {
	return nil
}

// Run queries Graphite for the metric.
func (p *Provider) Run(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric) v1alpha1.Measurement {
	startTime := timeutil.MetaNow()
	newMeasurement := v1alpha1.Measurement{
		StartedAt: &startTime,
	}

	result, err := p.api.Query(metric.Provider.Graphite.Query)
	if err != nil {
		return metricutil.MarkMeasurementError(newMeasurement, err)
	}

	if len(result) == 0 {
		return metricutil.MarkMeasurementError(newMeasurement, errors.New("no values found"))
	}

	newValue, newStatus, err := p.processResponse(metric, result)
	newMeasurement.Value = newValue
	if err != nil {
		return metricutil.MarkMeasurementError(newMeasurement, err)
	}

	newMeasurement.Phase = newStatus
	finishedTime := timeutil.MetaNow()
	newMeasurement.FinishedAt = &finishedTime

	return newMeasurement
}

// Resume should not be used with the Graphite provider since all the work should occur in the Run method
func (p *Provider) Resume(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, measurement v1alpha1.Measurement) v1alpha1.Measurement {
	p.logCtx.Warn("Graphite provider should not execute the Resume method")
	return measurement
}

// Terminate should not be used with the Graphite provider since all the work should occur in the Run method
func (p *Provider) Terminate(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, measurement v1alpha1.Measurement) v1alpha1.Measurement {
	p.logCtx.Warn("Graphite provider should not execute the Terminate method")
	return measurement
}

// GarbageCollect is a no-op for the Graphite provider
func (p *Provider) GarbageCollect(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, limit int) error {
	return nil
}

func (p *Provider) processResponse(metric v1alpha1.Metric, dataPoints []dataPoint) (string, v1alpha1.AnalysisPhase, error) {
	results := make([]float64, 0, len(dataPoints))
	valueStr := "["

	for _, dp := range dataPoints {
		if dp.Value != nil {
			valueStr = valueStr + fmt.Sprintf("%f,", *dp.Value)
			results = append(results, *dp.Value)
		}
	}

	// remove the last comma on the '[dp.Value,dp.Value,' string
	if len(valueStr) > 1 {
		valueStr = valueStr[:len(valueStr)-1]
	}

	valueStr = valueStr + "]"
	newStatus, err := evaluate.EvaluateResult(results, metric, p.logCtx)

	return valueStr, newStatus, err
}

// NewGraphiteProvider returns a new Graphite provider
func NewGraphiteProvider(api API, logCtx log.Entry) *Provider {
	return &Provider{
		logCtx: logCtx,
		api:    api,
	}
}
