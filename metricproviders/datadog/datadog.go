package datadog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	"github.com/argoproj/argo-rollouts/utils/evaluate"
	metricutil "github.com/argoproj/argo-rollouts/utils/metric"
	timeutil "github.com/argoproj/argo-rollouts/utils/time"

	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// This is done so we can explicitly override it in the unit test
var unixNow = func() int64 { return timeutil.Now().Unix() }

const (
	// ProviderType indicates the provider is datadog
	ProviderType            = "Datadog"
	DatadogTokensSecretName = "datadog"
	DatadogApiKey           = "api-key"
	DatadogAppKey           = "app-key"
	DatadogAddress          = "address"
)

// Provider contains all the required components to run a Datadog query
// Implements the Provider Interface
type Provider struct {
	logCtx log.Entry
	config datadogConfig
}

type datadogQueryAttributes struct {
	From     int64               `json:"from"`
	To       int64               `json:"to"`
	Queries  []map[string]string `json:"queries"`
	Formulas []map[string]string `json:"formulas"`
}

type datadogQuery struct {
	Attributes datadogQueryAttributes `json:"attributes"`
	QueryType  string                 `json:"type"`
}

type datadogRequest struct {
	Data datadogQuery `json:"data"`
}

type datadogResponseV1 struct {
	Series []struct {
		Pointlist [][]float64 `json:"pointlist"`
	}
}

type datadogResponseV2 struct {
	Data struct {
		Attributes struct {
			Columns []struct {
				Values []float64
			}
		}
		Errors string
	}
}

type datadogConfig struct {
	Address string `yaml:"address,omitempty"`
	ApiKey  string `yaml:"api-key,omitempty"`
	AppKey  string `yaml:"app-key,omitempty"`
}

// Type indicates provider is a Datadog provider
func (p *Provider) Type() string {
	return ProviderType
}

// GetMetadata returns any additional metadata which needs to be stored & displayed as part of the metrics result.
func (p *Provider) GetMetadata(metric v1alpha1.Metric) map[string]string {
	return nil
}

func (p *Provider) buildEndpointUrl(apiVersion string) (*url.URL, error) {
	endpoint := "https://api.datadoghq.com"
	if p.config.Address != "" {
		endpoint = p.config.Address
	}

	// Check if the user provided URL is valid first before adding the endpoint
	url, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}

	route := "/api/v1/query"
	if apiVersion == "v2" {
		route = "/api/v2/query/scalar"
	}

	// Add endpoint after getting the API version
	url, err = url.Parse(endpoint + route)
	if err != nil {
		return nil, err
	}
	return url, err
}

func (p *Provider) Run(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric) v1alpha1.Measurement {
	startTime := timeutil.MetaNow()
	dd := metric.Provider.Datadog

	if dd.ApiVersion == "v1" {
		p.logCtx.Warn("Datadog will soon deprecate their API v1. Please consider switching to v2 soon.")
	}

	// Measurement to pass back
	measurement := v1alpha1.Measurement{
		StartedAt: &startTime,
	}

	url, err := p.buildEndpointUrl(dd.ApiVersion)
	if err != nil {
		return metricutil.MarkMeasurementError(measurement, err)
	}

	// Interval default is in the spec. bigger things would need to fail to get here without an dd.Interval
	expDuration, err := dd.Interval.Duration()
	if err != nil {
		return metricutil.MarkMeasurementError(measurement, err)
	}
	// Convert to seconds as DataDog expects unix timestamp
	interval := int64(expDuration.Seconds())

	request, err := p.createRequest(dd, unixNow(), interval, url)
	if err != nil {
		return metricutil.MarkMeasurementError(measurement, err)
	}

	request.URL = url
	request.Header = make(http.Header)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("DD-API-KEY", p.config.ApiKey)
	request.Header.Set("DD-APPLICATION-KEY", p.config.AppKey)

	// Send Request
	httpClient := &http.Client{
		Timeout: time.Duration(10) * time.Second,
	}
	response, err := httpClient.Do(request)
	if err != nil {
		return metricutil.MarkMeasurementError(measurement, err)
	}

	value, status, err := p.parseResponse(metric, response, dd.ApiVersion)
	if err != nil {
		return metricutil.MarkMeasurementError(measurement, err)
	}

	measurement.Value = value
	measurement.Phase = status
	finishedTime := timeutil.MetaNow()
	measurement.FinishedAt = &finishedTime

	return measurement
}

func (p *Provider) createRequest(dd *v1alpha1.DatadogMetric, now int64, interval int64, url *url.URL) (*http.Request, error) {
	if dd.ApiVersion == "v1" {
		return p.createRequestV1(dd.Query, now, interval, url)
	}

	// we know dd.Query and dd.Queries are mutually exclusive.
	if dd.Query != "" {
		dd.Queries = map[string]string{"query": dd.Query}
	}

	return p.createRequestV2(dd.Queries, dd.Formula, now, interval, dd.Aggregator, url)
}

func (p *Provider) createRequestV1(query string, now int64, interval int64, url *url.URL) (*http.Request, error) {
	q := url.Query()
	q.Set("query", query)
	q.Set("from", strconv.FormatInt(now-interval, 10))
	q.Set("to", strconv.FormatInt(now, 10))
	url.RawQuery = q.Encode()

	return &http.Request{Method: "GET"}, nil
}

func buildQueriesPayload(queries map[string]string, aggregator string) []map[string]string {
	qp := make([]map[string]string, 0, len(queries))
	for k, v := range queries {
		p := map[string]string{
			"aggregator":  aggregator,
			"data_source": "metrics",
			"name":        k,
			"query":       v,
		}
		qp = append(qp, p)
	}
	return qp
}

func (p *Provider) createRequestV2(queries map[string]string, formula string, now int64, interval int64, aggregator string, url *url.URL) (*http.Request, error) {
	formulas := []map[string]string{}
	// ddAPI supports multiple formulas but doesn't make sense in our context
	// can't have a 'blank' formula, so have to guard
	if formula != "" {
		formulas = []map[string]string{{
			"formula": formula,
		}}
	}

	attribs := datadogQueryAttributes{
		// Datadog requires milliseconds for v2 api
		From:     (now - interval) * 1000,
		To:       now * 1000,
		Queries:  buildQueriesPayload(queries, aggregator),
		Formulas: formulas,
	}

	queryBody, err := json.Marshal(datadogRequest{
		Data: datadogQuery{
			QueryType:  "scalar_request",
			Attributes: attribs,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("Could not parse your JSON request: %v", err)
	}
	request := &http.Request{Method: "POST"}
	request.Body = io.NopCloser(bytes.NewReader(queryBody))
	return request, nil
}

func (p *Provider) parseResponse(metric v1alpha1.Metric, response *http.Response, apiVersion string) (string, v1alpha1.AnalysisPhase, error) {
	if apiVersion == "v1" {
		return p.parseResponseV1(metric, response)
	}
	return p.parseResponseV2(metric, response)
}

func (p *Provider) parseResponseV1(metric v1alpha1.Metric, response *http.Response) (string, v1alpha1.AnalysisPhase, error) {
	bodyBytes, err := io.ReadAll(response.Body)
	if err != nil {
		return "", v1alpha1.AnalysisPhaseError, fmt.Errorf("Received no bytes in response: %v", err)
	}

	if response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusUnauthorized {
		return "", v1alpha1.AnalysisPhaseError, fmt.Errorf("received authentication error response code: %v %s", response.StatusCode, string(bodyBytes))
	} else if response.StatusCode != http.StatusOK {
		return "", v1alpha1.AnalysisPhaseError, fmt.Errorf("received non 2xx response code: %v %s", response.StatusCode, string(bodyBytes))
	}

	var res datadogResponseV1
	err = json.Unmarshal(bodyBytes, &res)
	if err != nil {
		return "", v1alpha1.AnalysisPhaseError, fmt.Errorf("Could not parse JSON body: %v", err)
	}

	// Handle an empty query result
	if len(res.Series) == 0 || len(res.Series[0].Pointlist) == 0 {
		var nilFloat64 *float64
		status, err := evaluate.EvaluateResult(nilFloat64, metric, p.logCtx)
		seriesBytes, jsonErr := json.Marshal(res.Series)
		if jsonErr != nil {
			return "", v1alpha1.AnalysisPhaseError, fmt.Errorf("Failed to marshall JSON empty series: %v", jsonErr)
		}

		return string(seriesBytes), status, err
	}

	// Handle a populated query result
	series := res.Series[0]
	datapoint := series.Pointlist[len(series.Pointlist)-1]
	if len(datapoint) != 2 {
		return "", v1alpha1.AnalysisPhaseError, fmt.Errorf("Datapoint does not have 2 values")
	}

	value := datapoint[1]
	status, err := evaluate.EvaluateResult(value, metric, p.logCtx)
	return strconv.FormatFloat(value, 'f', -1, 64), status, err
}

func (p *Provider) parseResponseV2(metric v1alpha1.Metric, response *http.Response) (string, v1alpha1.AnalysisPhase, error) {
	bodyBytes, err := io.ReadAll(response.Body)
	if err != nil {
		return "", v1alpha1.AnalysisPhaseError, fmt.Errorf("Received no bytes in response: %v", err)
	}

	if response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusUnauthorized {
		return "", v1alpha1.AnalysisPhaseError, fmt.Errorf("received authentication error response code: %v %s", response.StatusCode, string(bodyBytes))
	} else if response.StatusCode != http.StatusOK {
		return "", v1alpha1.AnalysisPhaseError, fmt.Errorf("received non 2xx response code: %v %s", response.StatusCode, string(bodyBytes))
	}

	var res datadogResponseV2
	err = json.Unmarshal(bodyBytes, &res)
	if err != nil {
		return "", v1alpha1.AnalysisPhaseError, fmt.Errorf("Could not parse JSON body: %v", err)
	}

	// Handle an error returned by Datadog
	if res.Data.Errors != "" {
		return "", v1alpha1.AnalysisPhaseError, fmt.Errorf("There were errors in your query: %v", res.Data.Errors)
	}

	// Handle an empty query result
	if reflect.ValueOf(res.Data.Attributes).IsZero() || len(res.Data.Attributes.Columns) == 0 || len(res.Data.Attributes.Columns[0].Values) == 0 {
		var nilFloat64 *float64
		status, err := evaluate.EvaluateResult(nilFloat64, metric, p.logCtx)

		var attributesBytes []byte
		var jsonErr error
		// Should be impossible for this to not be true, based on dd openapi spec.
		// But in this case, better safe than sorry
		if len(res.Data.Attributes.Columns) == 1 {
			attributesBytes, jsonErr = json.Marshal(res.Data.Attributes.Columns[0].Values)
		} else {
			attributesBytes, jsonErr = json.Marshal(res.Data.Attributes)
		}

		if jsonErr != nil {
			return "", v1alpha1.AnalysisPhaseError, fmt.Errorf("Failed to marshall JSON empty Values: %v", jsonErr)
		}

		return string(attributesBytes), status, err
	}

	// Handle a populated query result
	column := res.Data.Attributes.Columns[0]
	value := column.Values[0]
	status, err := evaluate.EvaluateResult(value, metric, p.logCtx)
	return strconv.FormatFloat(value, 'f', -1, 64), status, err
}

// Resume should not be used the Datadog provider since all the work should occur in the Run method
func (p *Provider) Resume(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, measurement v1alpha1.Measurement) v1alpha1.Measurement {
	p.logCtx.Warn("Datadog provider should not execute the Resume method")
	return measurement
}

// Terminate should not be used the Datadog provider since all the work should occur in the Run method
func (p *Provider) Terminate(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, measurement v1alpha1.Measurement) v1alpha1.Measurement {
	p.logCtx.Warn("Datadog provider should not execute the Terminate method")
	return measurement
}

// GarbageCollect is a no-op for the Datadog provider
func (p *Provider) GarbageCollect(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, limit int) error {
	return nil
}

func lookupKeysInEnv(keys []string) map[string]string {
	valuesByKey := make(map[string]string)
	for i := range keys {
		key := keys[i]
		formattedKey := strings.ToUpper(strings.ReplaceAll(key, "-", "_"))
		if value, ok := os.LookupEnv(fmt.Sprintf("DD_%s", formattedKey)); ok {
			valuesByKey[key] = value
		}
	}
	return valuesByKey
}

// The current gen tooling we are using can't generate CRD with all the validations we need.
// This is unfortunate, user has more ways to deliver an invalid Analysis Template vs
// being rejected on delivery by k8s (and allowing for a validation step if desired in CI/CD).
// So we run through all the checks here. If the situation changes (eg: being able to use oneOf with required)
// in the CRD spec, please update.
func validateIncomingProps(dd *v1alpha1.DatadogMetric) error {
	// check that we have the required field
	if dd.Query == "" && len(dd.Queries) == 0 {
		return errors.New("Must have either a query or queries. Please review the Analysis Template.")
	}

	// check that we have ONE OF query/queries
	if dd.Query != "" && len(dd.Queries) > 0 {
		return errors.New("Cannot have both a query and queries. Please review the Analysis Template.")
	}

	// check that query is set for apiversion v1
	if dd.ApiVersion == "v1" && dd.Query == "" {
		return errors.New("Query is empty. API Version v1 only supports using the query parameter in your Analysis Template.")
	}

	// formula <3 queries. won't go anywhere without them
	if dd.Formula != "" && len(dd.Queries) == 0 {
		return errors.New("Formula are only valid when queries are set. Please review the Analysis Template.")
	}

	// Reject queries with more than 1 when NO formula provided. While this would technically work
	// DD will return 2 columns of data, and there is no guarantee what order they would be in, so
	// there is no way to guess at intention of user. Since this is about metrics and monitoring, we should
	// avoid ambiguity.
	if dd.Formula == "" && len(dd.Queries) > 1 {
		return errors.New("When multiple queries are provided you must include a formula.")
	}

	if dd.ApiVersion == "v1" && dd.Aggregator != "" {
		return errors.New("Aggregator is not supported in v1. Please review the Analysis Template.")
	}

	return nil
}

func NewDatadogProvider(logCtx log.Entry, kubeclientset kubernetes.Interface, metric v1alpha1.Metric) (*Provider, error) {
	ns := defaults.Namespace()

	apiKey := ""
	appKey := ""
	address := ""
	secretKeys := []string{DatadogApiKey, DatadogAppKey, DatadogAddress}
	envValuesByKey := lookupKeysInEnv(secretKeys)
	if len(envValuesByKey) == len(secretKeys) {
		apiKey = envValuesByKey[DatadogApiKey]
		appKey = envValuesByKey[DatadogAppKey]
		address = envValuesByKey[DatadogAddress]
	} else {
		secret, err := kubeclientset.CoreV1().Secrets(ns).Get(context.TODO(), DatadogTokensSecretName, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		apiKey = string(secret.Data[DatadogApiKey])
		appKey = string(secret.Data[DatadogAppKey])
		if _, hasAddress := secret.Data[DatadogAddress]; hasAddress {
			address = string(secret.Data[DatadogAddress])
		}
	}

	if apiKey != "" && appKey != "" {

		err := validateIncomingProps(metric.Provider.Datadog)
		if err != nil {
			return nil, err
		}

		return &Provider{
			logCtx: logCtx,
			config: datadogConfig{
				Address: address,
				ApiKey:  apiKey,
				AppKey:  appKey,
			},
		}, nil
	} else {
		return nil, errors.New("API or App token not found")
	}
}
