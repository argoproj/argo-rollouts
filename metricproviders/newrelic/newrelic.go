package newrelic

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/evaluate"
	metricutil "github.com/argoproj/argo-rollouts/utils/metric"
	timeutil "github.com/argoproj/argo-rollouts/utils/time"
	"github.com/argoproj/argo-rollouts/utils/version"
	"github.com/newrelic/newrelic-client-go/newrelic"
	"github.com/newrelic/newrelic-client-go/pkg/nrdb"
	log "github.com/sirupsen/logrus"
)

const (
	//ProviderType indicates the provider is wavefront
	ProviderType                               = "NewRelic"
	repoURL                                    = "https://github.com/argoproj/argo-rollouts"
	EnvVarArgoRolloutsNewRelicApiKey           = "ARGO_ROLLOUTS_NEWRELIC_APIKEY"
	EnvVarArgoRolloutsNewRelicAccountId        = "ARGO_ROLLOUTS_NEWRELIC_ACCOUNT_ID"
	EnvVarArgoRolloutsNewRelicBaseUrlRest      = "ARGO_ROLLOUTS_NEWRELIC_BASE_URL_REST"
	EnvVarArgoRolloutsNewRelicBaseUrlNerdGraph = "ARGO_ROLLOUTS_NEWRELIC_BASE_URL_NERD_GRAPH"
	EnvVarArgoRolloutsNewRelicRegion           = "ARGO_ROLLOUTS_NEWRELIC_REGION"
)

var userAgent = fmt.Sprintf("argo-rollouts/%s (%s)", version.GetVersion(), repoURL)

type NewRelicClientAPI interface {
	Query(query string) ([]nrdb.NRDBResult, error)
}

type NewRelicClient struct {
	*newrelic.NewRelic
	AccountID int
}

//Query executes a NRQL query against the given New Relic account
func (n *NewRelicClient) Query(query string) ([]nrdb.NRDBResult, error) {
	results, err := n.Nrdb.Query(n.AccountID, nrdb.NRQL(query))
	if err != nil {
		return nil, err
	}
	// TODO(jwelch) return metadata from NRDBResultContainer to include on the measurement
	return results.Results, nil
}

type Provider struct {
	api    NewRelicClientAPI
	logCtx log.Entry
}

// Run queries NewRelic for the metric
func (p *Provider) Run(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric) v1alpha1.Measurement {
	startTime := timeutil.MetaNow()
	newMeasurement := v1alpha1.Measurement{
		StartedAt: &startTime,
	}

	results, err := p.api.Query(metric.Provider.NewRelic.Query)
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

func (p *Provider) processResponse(metric v1alpha1.Metric, results []nrdb.NRDBResult) (string, v1alpha1.AnalysisPhase, error) {
	if len(results) == 1 {
		result := results[0]
		if len(result) == 0 {
			return "", v1alpha1.AnalysisPhaseFailed, fmt.Errorf("no results returned from NRQL query")
		}
		valueStr, err := toJSONString(result)
		if err != nil {
			return "", v1alpha1.AnalysisPhaseError, fmt.Errorf("could not marshal results: %w", err)
		}
		newStatus, err := evaluate.EvaluateResult(result, metric, p.logCtx)
		return valueStr, newStatus, err
	} else if len(results) > 1 {
		valueStr, err := toJSONString(results)
		if err != nil {
			return "", v1alpha1.AnalysisPhaseError, fmt.Errorf("could not marshal results: %w", err)
		}
		newStatus, err := evaluate.EvaluateResult(results, metric, p.logCtx)
		return valueStr, newStatus, err
	} else {
		return "", v1alpha1.AnalysisPhaseFailed, fmt.Errorf("no results returned from NRQL query")
	}
}

// Resume should not be used the NewRelic provider since all the work should occur in the Run method
func (p *Provider) Resume(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, measurement v1alpha1.Measurement) v1alpha1.Measurement {
	p.logCtx.Warn("NewRelic provider should not execute the Resume method")
	return measurement
}

// Terminate should not be used the NewRelic provider since all the work should occur in the Run method
func (p *Provider) Terminate(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, measurement v1alpha1.Measurement) v1alpha1.Measurement {
	p.logCtx.Warn("NewRelic provider should not execute the Terminate method")
	return measurement
}

// GarbageCollect should not be used the NewRelic provider since all the work should occur in the Run method
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

//NewNewRelicProvider creates a new NewRelic provider
func NewNewRelicProvider(api NewRelicClientAPI, logCtx log.Entry) *Provider {
	return &Provider{
		logCtx: logCtx,
		api:    api,
	}
}

//NewNewRelicAPIClient creates a new NewRelic API client from metric configuration
func NewNewRelicAPIClient(metric v1alpha1.Metric) (NewRelicClientAPI, error) {
	envValuesByKey := make(map[string]string)
	if apiKey, ok := os.LookupEnv(fmt.Sprintf("%s", EnvVarArgoRolloutsNewRelicApiKey)); ok {
		envValuesByKey[EnvVarArgoRolloutsNewRelicApiKey] = apiKey
		log.Debugf("ARGO_ROLLOUTS_WAVEFRONT_ADDRESS: %v", envValuesByKey[EnvVarArgoRolloutsNewRelicApiKey])
	}
	if accountId, ok := os.LookupEnv(fmt.Sprintf("%s", EnvVarArgoRolloutsNewRelicAccountId)); ok {
		envValuesByKey[EnvVarArgoRolloutsNewRelicAccountId] = accountId
		log.Debugf("ARGO_ROLLOUTS_WAVEFRONT_ADDRESS: %v", envValuesByKey[EnvVarArgoRolloutsNewRelicAccountId])
	}
	if region, ok := os.LookupEnv(fmt.Sprintf("%s", EnvVarArgoRolloutsNewRelicRegion)); ok {
		envValuesByKey[EnvVarArgoRolloutsNewRelicRegion] = region
		log.Debugf("ARGO_ROLLOUTS_WAVEFRONT_ADDRESS: %v", envValuesByKey[EnvVarArgoRolloutsNewRelicRegion])
	}
	if baseUrlRest, ok := os.LookupEnv(fmt.Sprintf("%s", EnvVarArgoRolloutsNewRelicBaseUrlRest)); ok {
		envValuesByKey[EnvVarArgoRolloutsNewRelicBaseUrlRest] = baseUrlRest
		log.Debugf("ARGO_ROLLOUTS_WAVEFRONT_ADDRESS: %v", envValuesByKey[EnvVarArgoRolloutsNewRelicBaseUrlRest])
	}
	if baseUrlNerdGraph, ok := os.LookupEnv(fmt.Sprintf("%s", EnvVarArgoRolloutsNewRelicBaseUrlNerdGraph)); ok {
		envValuesByKey[EnvVarArgoRolloutsNewRelicBaseUrlNerdGraph] = baseUrlNerdGraph
		log.Debugf("ARGO_ROLLOUTS_WAVEFRONT_ADDRESS: %v", envValuesByKey[EnvVarArgoRolloutsNewRelicBaseUrlNerdGraph])
	}
	var newrelicOptions []newrelic.ConfigOption
	if len(envValuesByKey[EnvVarArgoRolloutsNewRelicApiKey]) != 0 {
		newrelicOptions = []newrelic.ConfigOption{newrelic.ConfigPersonalAPIKey(envValuesByKey[EnvVarArgoRolloutsNewRelicApiKey]), newrelic.ConfigUserAgent(userAgent)}
	}
	if len(envValuesByKey[EnvVarArgoRolloutsNewRelicRegion]) != 0 {
		newrelicOptions = append(newrelicOptions, newrelic.ConfigRegion(envValuesByKey[EnvVarArgoRolloutsNewRelicRegion]))
	} else {
		envValuesByKey[EnvVarArgoRolloutsNewRelicRegion] = "us"
		newrelicOptions = append(newrelicOptions, newrelic.ConfigRegion(envValuesByKey[EnvVarArgoRolloutsNewRelicRegion]))
	}
	// base URL for the new relic REST API
	if len(envValuesByKey[EnvVarArgoRolloutsNewRelicBaseUrlRest]) != 0 {
		newrelicOptions = append(newrelicOptions, newrelic.ConfigBaseURL(envValuesByKey[EnvVarArgoRolloutsNewRelicBaseUrlRest]))
	}
	// base URL for the nerdgraph (graphQL) API
	if len(envValuesByKey[EnvVarArgoRolloutsNewRelicBaseUrlNerdGraph]) != 0 {
		newrelicOptions = append(newrelicOptions, newrelic.ConfigNerdGraphBaseURL(envValuesByKey[EnvVarArgoRolloutsNewRelicBaseUrlNerdGraph]))
	}
	if envValuesByKey[EnvVarArgoRolloutsNewRelicApiKey] != "" && envValuesByKey[EnvVarArgoRolloutsNewRelicAccountId] != "" {
		nrClient, err := newrelic.New(newrelicOptions...)
		if err != nil {
			return nil, err
		}
		accID, err := strconv.Atoi(envValuesByKey[EnvVarArgoRolloutsNewRelicAccountId])
		if err != nil {
			return nil, fmt.Errorf("could not parse account ID: %w", err)
		}
		return &NewRelicClient{NewRelic: nrClient, AccountID: accID}, nil
	} else {
		return nil, errors.New("account ID or personal API key not found")
	}
}
