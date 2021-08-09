package graphite

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/evaluate"
	metricutil "github.com/argoproj/argo-rollouts/utils/metric"
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
		logCTX:  logCTX,
		client:  http.DefaultClient,
		url:     *graphiteURL,
		timeout: 5 * time.Second,
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

// Run queries Graphite for the metric.
func (p *Provider) Run(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric) v1alpha1.Measurement {
	startTime := metav1.Now()
	newMeasurement := v1alpha1.Measurement{
		StartedAt: &startTime,
	}

	value, err := p.api.Query(metric.Provider.Graphite.Query)
	if err != nil {
		return metricutil.MarkMeasurementError(newMeasurement, err)
	}

	if value == nil {
		return metricutil.MarkMeasurementError(newMeasurement, errors.New("no values found"))
	}

	newMeasurement.Value = fmt.Sprintf("%.3f", *value)

	newStatus, err := evaluate.EvaluateResult(*value, metric, p.logCtx)
	if err != nil {
		return metricutil.MarkMeasurementError(newMeasurement, err)
	}

	newMeasurement.Phase = newStatus
	finishedTime := metav1.Now()
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

// GarbageCollect is a no-op for the prometheus provider
func (p *Provider) GarbageCollect(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, limit int) error {
	return nil
}

// NewGraphiteProvider returns a new Graphite provider
func NewGraphiteProvider(api API, logCtx log.Entry) *Provider {
	return &Provider{
		logCtx: logCtx,
		api:    api,
	}
}
