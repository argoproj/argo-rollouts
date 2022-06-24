package influxdb

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/argoproj/argo-rollouts/utils/defaults"
	"github.com/argoproj/argo-rollouts/utils/evaluate"
	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	influxapi "github.com/influxdata/influxdb-client-go/v2/api"
	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	metricutil "github.com/argoproj/argo-rollouts/utils/metric"
)

const (
	//ProviderType indicates the provider is InfluxDB
	ProviderType = "Influxdb"
	//DefaultInfluxdbTokensSecretName is the k8s secret that has InfluxDB api token, org and address
	DefaultInfluxdbTokensSecretName = "influxdb"
	influxdbToken                   = "authToken"
	influxdbOrg                     = "org"
	influxdbAddress                 = "address"
	defaultQueryTimeout             = 30 * time.Second
)

// Provider contains all the required components to run a influxdb flux query
type Provider struct {
	api    influxapi.QueryAPI
	logCtx log.Entry
}

// Type indicates provider is a influxdb provider
func (p *Provider) Type() string {
	return ProviderType
}

// GetMetadata returns any additional metadata which needs to be stored & displayed as part of the metrics result.
func (p *Provider) GetMetadata(metric v1alpha1.Metric) map[string]string {
	return nil
}

// Run queries influxdb for the metric
func (p *Provider) Run(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric) v1alpha1.Measurement {
	startTime := metav1.Now()
	newMeasurement := v1alpha1.Measurement{
		StartedAt: &startTime,
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultQueryTimeout)
	defer cancel()
	result, err := p.api.Query(ctx, metric.Provider.Influxdb.Query)
	if err != nil {
		return metricutil.MarkMeasurementError(newMeasurement, err)
	}
	newValue, newStatus, err := p.processResponse(metric, result)
	if err != nil {
		return metricutil.MarkMeasurementError(newMeasurement, err)
	}
	newMeasurement.Value = newValue

	newMeasurement.Phase = newStatus
	finishedTime := metav1.Now()
	newMeasurement.FinishedAt = &finishedTime
	return newMeasurement
}

// Resume should not be used by the influxdb provider since all the work should occur in the Run method.
func (p *Provider) Resume(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, measurement v1alpha1.Measurement) v1alpha1.Measurement {
	p.logCtx.Warn("Influxdb provider should not execute the Resume method")
	return measurement
}

// Terminate should not be used by the influxdb provider since all the work should occur in the Run method.
func (p *Provider) Terminate(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, measurement v1alpha1.Measurement) v1alpha1.Measurement {
	p.logCtx.Warn("Influxdb provider should not execute the Terminate method")
	return measurement
}

// GarbageCollect is a no-op for the influxdb provider
func (p *Provider) GarbageCollect(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, limit int) error {
	return nil
}

func (p *Provider) processResponse(metric v1alpha1.Metric, result *influxapi.QueryTableResult) (string, v1alpha1.AnalysisPhase, error) {
	var res []interface{}
	if result == nil {
		return "", v1alpha1.AnalysisPhaseError, fmt.Errorf("no QueryTableResult returned from flux query")
	}
	for result.Next() {
		res = append(res, result.Record().Value())
	}
	if len(res) == 0 {
		return "", v1alpha1.AnalysisPhaseError, fmt.Errorf("no results returned from flux query")
	}
	status, err := evaluate.EvaluateResult(res, metric, p.logCtx)
	if err != nil {
		return "", v1alpha1.AnalysisPhaseError, err
	}
	return fmt.Sprint(res), status, err
}

// NewInfluxdbProvider Creates a new Influxdb client
func NewInfluxdbProvider(api influxapi.QueryAPI, logCtx log.Entry) *Provider {
	return &Provider{
		logCtx: logCtx,
		api:    api,
	}
}

// NewInfluxdbAPI generates a Influx API from the metric configuration
func NewInfluxdbAPI(metric v1alpha1.Metric, kubeclientset kubernetes.Interface) (influxapi.QueryAPI, error) {
	profileSecret := DefaultInfluxdbTokensSecretName
	if metric.Provider.Influxdb.Profile != "" {
		profileSecret = metric.Provider.Influxdb.Profile
	}
	ns := defaults.Namespace()
	secret, err := kubeclientset.CoreV1().Secrets(ns).Get(context.TODO(), profileSecret, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	authToken := string(secret.Data[influxdbToken])
	address := string(secret.Data[influxdbAddress])
	org := string(secret.Data[influxdbOrg])

	if authToken != "" && address != "" && org != "" {
		influxClient := influxdb2.NewClient(address, authToken)
		return influxClient.QueryAPI(org), nil
	}

	return nil, errors.New("authToken, org, or address not found")
}
