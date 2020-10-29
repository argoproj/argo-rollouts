package newrelic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"strings"

	"github.com/newrelic/newrelic-client-go/newrelic"
	"github.com/newrelic/newrelic-client-go/pkg/nrdb"
	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/evaluate"
	metricutil "github.com/argoproj/argo-rollouts/utils/metric"
	"github.com/argoproj/argo-rollouts/utils/version"
)

const (
	//ProviderType indicates the provider is wavefront
	ProviderType                     = "NewRelic"
	DefaultNewRelicProfileSecretName = "newrelic"
	repoURL                          = "https://github.com/argoproj/argo-rollouts"
)

var userAgent = fmt.Sprintf("argo-rollouts/%s (%s)", version.GetVersion(), repoURL)

type NewRelicClientAPI interface {
	Query(query string) ([]nrdb.NrdbResult, error)
}

type NewRelicClient struct {
	*newrelic.NewRelic
	AccountID int
}

//Query executes a NRQL query against the given New Relic account
func (n *NewRelicClient) Query(query string) ([]nrdb.NrdbResult, error) {
	results, err := n.Nrdb.Query(n.AccountID, nrdb.Nrql(query))
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
	startTime := metav1.Now()
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

	finishedTime := metav1.Now()
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

func (p *Provider) processResponse(metric v1alpha1.Metric, results []nrdb.NrdbResult) (string, v1alpha1.AnalysisPhase, error) {
	if len(results) == 1 {
		result := results[0]
		if len(result) == 0 {
			return "", v1alpha1.AnalysisPhaseFailed, fmt.Errorf("no results returned from NRQL query")
		}
		valueStr, err := toJSONString(result)
		if err != nil {
			return "", v1alpha1.AnalysisPhaseError, fmt.Errorf("could not marshal results: %w", err)
		}
		newStatus := evaluate.EvaluateResult(result, metric, p.logCtx)
		return valueStr, newStatus, nil
	} else if len(results) > 1 {
		valueStr, err := toJSONString(results)
		if err != nil {
			return "", v1alpha1.AnalysisPhaseError, fmt.Errorf("could not marshal results: %w", err)
		}
		newStatus := evaluate.EvaluateResult(results, metric, p.logCtx)
		return valueStr, newStatus, nil
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

//NewNewRelicProvider creates a new NewRelic provider
func NewNewRelicProvider(api NewRelicClientAPI, logCtx log.Entry) *Provider {
	return &Provider{
		logCtx: logCtx,
		api:    api,
	}
}

//NewNewRelicAPIClient creates a new NewRelic API client from metric configuration
func NewNewRelicAPIClient(metric v1alpha1.Metric, kubeclientset kubernetes.Interface) (NewRelicClientAPI, error) {
	ns := Namespace()
	secretName := DefaultNewRelicProfileSecretName
	if metric.Provider.NewRelic.ProfileSecretName != "" {
		secretName = metric.Provider.NewRelic.ProfileSecretName
	}
	secret, err := kubeclientset.CoreV1().Secrets(ns).Get(context.TODO(), secretName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	apiKey := string(secret.Data["personal-api-key"])
	accountID := string(secret.Data["account-id"])
	region := "us"
	if _, ok := secret.Data["region"]; ok {
		region = string(secret.Data["region"])
	}

	if apiKey != "" && accountID != "" {
		nrClient, err := newrelic.New(newrelic.ConfigPersonalAPIKey(apiKey), newrelic.ConfigRegion(region), newrelic.ConfigUserAgent(userAgent))
		if err != nil {
			return nil, err
		}
		accID, err := strconv.Atoi(accountID)
		if err != nil {
			return nil, fmt.Errorf("could not parse account ID: %w", err)
		}
		return &NewRelicClient{NewRelic: nrClient, AccountID: accID}, nil
	} else {
		return nil, errors.New("account ID or personal API key not found")
	}
}

func Namespace() string {
	// This way assumes you've set the POD_NAMESPACE environment variable using the downward API.
	// This check has to be done first for backwards compatibility with the way InClusterConfig was originally set up
	if ns, ok := os.LookupEnv("POD_NAMESPACE"); ok {
		return ns
	}
	// Fall back to the namespace associated with the service account token, if available
	if data, err := ioutil.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace"); err == nil {
		if ns := strings.TrimSpace(string(data)); len(ns) > 0 {
			return ns
		}
	}
	return "argo-rollouts"
}
