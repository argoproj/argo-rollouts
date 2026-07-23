package datadog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	"github.com/argoproj/argo-rollouts/utils/evaluate"
	metricutil "github.com/argoproj/argo-rollouts/utils/metric"
	timeutil "github.com/argoproj/argo-rollouts/utils/time"

	log "github.com/sirupsen/logrus"
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

const (
	metadataHTTPStatusCode   = "httpStatusCode"
	metadataRequestOutcome   = "requestOutcome"
	metadataResponseReceived = "responseReceived"

	requestOutcomeHTTPError         = "http_error"
	requestOutcomeResponseError     = "response_error"
	requestOutcomeResponseReadError = "response_read_error"
	requestOutcomeSuccess           = "success"
	requestOutcomeTimeout           = "timeout"
	requestOutcomeTransportError    = "transport_error"
)

type responseReadError struct {
	err error
}

func (e *responseReadError) Error() string {
	return fmt.Sprintf("Failed to read Datadog API response body: %v", e.err)
}

func (e *responseReadError) Unwrap() error {
	return e.err
}

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
			Columns []datadogV2Column
		}
		Errors string
	}
}

// Column types returned by the v2 /query/scalar endpoint. The response is a
// oneOf discriminated by this field (see Datadog's GroupScalarColumn /
// DataScalarColumn schemas): a number column carries the formula/query results
// as floats, a group column carries the tag values when the query uses `by
// {tag}`.
const (
	datadogV2ColumnNumber = "number"
	datadogV2ColumnGroup  = "group"
)

// datadogV2Column is a single column of a v2 scalar response. Values are kept
// as raw JSON because the shape differs by Type: a number column holds floats
// (or null), while a group column holds an array of tag values per group
// (`[][]string` on the wire). A `by {tag}` query returns one group column *per
// tag dimension* — `by {region, host}` yields a separate `region` column and
// `host` column — so the group columns must be read together to reconstruct a
// row's full label. The inner array is tag aliasing within a single dimension,
// not the multiple tags of a multi-tag query.
type datadogV2Column struct {
	Name   string            `json:"name"`
	Type   string            `json:"type"`
	Values []json.RawMessage `json:"values"`
}

// groupedValue pairs a numeric value with its group tag (if the query was
// grouped by a tag) so the two stay aligned even when null entries are
// skipped.
type groupedValue struct {
	Name  string  `json:"name"`
	Value float64 `json:"value"`
}

func isJSONNull(r json.RawMessage) bool {
	return string(bytes.TrimSpace(r)) == "null"
}

// findColumns locates the number column and every group column in a v2 scalar
// response. A response may carry several number columns (one per formula or
// query); like the original implementation we evaluate the first. Every group
// column is returned, in response order, because a multi-tag query (`by
// {region, host}`) reports one group column per tag and a row's label is built
// by joining across them. Legacy responses omit the type field, so an empty
// type is treated as numeric for backward compatibility with the previous
// behavior of picking column 0.
func findColumns(columns []datadogV2Column) (numCol *datadogV2Column, groupCols []*datadogV2Column) {
	for i := range columns {
		c := &columns[i]
		if numCol == nil && (c.Type == "" || c.Type == datadogV2ColumnNumber) {
			numCol = c
		}
		if c.Type == datadogV2ColumnGroup {
			groupCols = append(groupCols, c)
		}
	}
	return
}

// groupNameAt returns the label for the group at idx, joined across every group
// column. Each column contributes the tag value(s) it recorded for that row, so
// `by {region, host}` yields `us-east,host-a`. A column's per-row entry is
// itself an array because a single dimension can carry aliased values; those
// are joined in too. ok is false when any column is missing the row or its
// entry is malformed or empty (including a JSON `null` or `[]`), which callers
// treat as an error rather than silently pairing a value with a blank label.
func groupNameAt(cols []*datadogV2Column, idx int) (string, bool) {
	parts := make([]string, 0, len(cols))
	for _, col := range cols {
		if idx >= len(col.Values) {
			return "", false
		}
		var tags []string
		if err := json.Unmarshal(col.Values[idx], &tags); err != nil || len(tags) == 0 {
			return "", false
		}
		parts = append(parts, tags...)
	}
	return strings.Join(parts, ","), true
}

// extractValues walks the v2 scalar response and returns the numeric values,
// their group labels, and whether the query was grouped (i.e. at least one
// group column was present). Callers dispatch on grouped — not on the number of
// values — so a `by {tag}` query that happens to match a single group is still
// treated as a slice, mirroring how the Prometheus provider keys off the
// response type.
//
// Null entries in the number column are Datadog's "no data for this group"
// marker and are skipped together with their group label so values and groups
// stay index-aligned. A non-numeric value, or a group label that fails to
// parse, is surfaced as an error rather than silently dropped — for a
// production rollout gate we'd rather fail loudly than evaluate partial data.
func extractValues(columns []datadogV2Column) (values []float64, groups []groupedValue, grouped bool, err error) {
	numCol, groupCols := findColumns(columns)
	grouped = len(groupCols) > 0
	if numCol == nil {
		return nil, nil, grouped, nil
	}
	values = make([]float64, 0, len(numCol.Values))
	for i, r := range numCol.Values {
		if isJSONNull(r) {
			continue
		}
		var f float64
		if err := json.Unmarshal(r, &f); err != nil {
			return nil, nil, grouped, fmt.Errorf("could not parse numeric value %q in column %q: %v", string(r), numCol.Name, err)
		}
		values = append(values, f)
		if grouped {
			name, ok := groupNameAt(groupCols, i)
			if !ok {
				return nil, nil, grouped, fmt.Errorf("could not parse group label at index %d", i)
			}
			groups = append(groups, groupedValue{Name: name, Value: f})
		}
	}
	return values, groups, grouped, nil
}

// maxGroupsInMetadata bounds how many group entries are persisted to the
// measurement metadata. metadata.groups is display-only — evaluation always
// uses the full values slice — so trimming it is safe. A high-cardinality tag
// like `by {host}` can return thousands of groups, and every measurement is
// stored in the AnalysisRun status up to DefaultMeasurementHistoryLimit times;
// left unbounded that can exceed the ~1.5MB Kubernetes object limit and wedge
// the run with "request entity too large". When we trim, we keep the groups at
// both value extremes and flag the trim.
const maxGroupsInMetadata = 100

// groupsMetadata renders the group pairs as JSON for the measurement metadata,
// capping the entry count at maxGroupsInMetadata and reporting whether the list
// was truncated. Only the display-only breakdown is trimmed; the evaluated
// values slice is left intact.
//
// The offending group is the highest value for an error-rate or latency gate
// but the lowest for a success-rate gate, and this function can't see the
// condition. So when trimming we keep both extremes — the lowest and highest
// halves — which keeps the worst outlier in view either way; the dropped
// middle is the "normal" groups an operator doesn't need.
func groupsMetadata(groups []groupedValue) (groupsJSON string, truncated bool) {
	if len(groups) > maxGroupsInMetadata {
		sorted := make([]groupedValue, len(groups))
		copy(sorted, groups)
		sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Value < sorted[j].Value })
		low := maxGroupsInMetadata / 2
		high := maxGroupsInMetadata - low
		groups = append(sorted[:low:low], sorted[len(sorted)-high:]...)
		truncated = true
	}
	out, _ := json.Marshal(groups)
	return string(out), truncated
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

	timeout, err := requestTimeout(dd)
	if err != nil {
		return metricutil.MarkMeasurementError(measurement, err)
	}

	response, metadata, err := sendRequest(request, timeout)
	measurement.Metadata = metadata
	if err != nil {
		return metricutil.MarkMeasurementError(measurement, err)
	}
	defer response.Body.Close()

	value, status, responseMetadata, err := p.parseResponse(metric, response, dd.ApiVersion)
	mergeMetadata(measurement.Metadata, responseMetadata)
	if err != nil {
		setResponseErrorOutcome(measurement.Metadata, response.StatusCode, err)
		return metricutil.MarkMeasurementError(measurement, err)
	}

	measurement.Value = value
	measurement.Phase = status
	measurement.Metadata[metadataRequestOutcome] = requestOutcomeSuccess
	finishedTime := timeutil.MetaNow()
	measurement.FinishedAt = &finishedTime

	return measurement
}

func requestTimeout(dd *v1alpha1.DatadogMetric) (time.Duration, error) {
	const defaultTimeout = 10 * time.Second
	if dd.RequestTimeout == "" {
		return defaultTimeout, nil
	}
	return dd.RequestTimeout.Duration()
}

func sendRequest(request *http.Request, timeout time.Duration) (*http.Response, map[string]string, error) {
	response, err := (&http.Client{Timeout: timeout}).Do(request)
	if err != nil {
		return nil, requestErrorMetadata(err), formatRequestError(err, timeout)
	}
	return response, map[string]string{
		metadataHTTPStatusCode:   strconv.Itoa(response.StatusCode),
		metadataResponseReceived: strconv.FormatBool(true),
	}, nil
}

func requestErrorMetadata(err error) map[string]string {
	outcome := requestOutcomeTransportError
	if isTimeoutError(err) {
		outcome = requestOutcomeTimeout
	}
	return map[string]string{
		metadataRequestOutcome:   outcome,
		metadataResponseReceived: strconv.FormatBool(false),
	}
}

func formatRequestError(err error, timeout time.Duration) error {
	if isTimeoutError(err) {
		return fmt.Errorf("Datadog API request timed out after %s before receiving an HTTP response: %w", timeout, err)
	}
	return fmt.Errorf("Datadog API request failed before receiving an HTTP response: %w", err)
}

func mergeMetadata(destination, source map[string]string) {
	for key, value := range source {
		destination[key] = value
	}
}

func setResponseErrorOutcome(metadata map[string]string, statusCode int, err error) {
	metadata[metadataRequestOutcome] = requestOutcomeResponseError
	if statusCode != http.StatusOK {
		metadata[metadataRequestOutcome] = requestOutcomeHTTPError
	}
	var readErr *responseReadError
	if errors.As(err, &readErr) {
		metadata[metadataRequestOutcome] = requestOutcomeResponseReadError
	}
}

func isTimeoutError(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
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
	// we cannot leave aggregator empty as it will be passed as such to datadog API and fail
	if aggregator == "" {
		aggregator = "last"
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

func (p *Provider) parseResponse(metric v1alpha1.Metric, response *http.Response, apiVersion string) (string, v1alpha1.AnalysisPhase, map[string]string, error) {
	if apiVersion == "v1" {
		value, phase, err := p.parseResponseV1(metric, response)
		return value, phase, nil, err
	}
	return p.parseResponseV2(metric, response)
}

func (p *Provider) parseResponseV1(metric v1alpha1.Metric, response *http.Response) (string, v1alpha1.AnalysisPhase, error) {
	bodyBytes, err := io.ReadAll(response.Body)
	if err != nil {
		return "", v1alpha1.AnalysisPhaseError, &responseReadError{err: err}
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

func (p *Provider) parseResponseV2(metric v1alpha1.Metric, response *http.Response) (string, v1alpha1.AnalysisPhase, map[string]string, error) {
	bodyBytes, err := io.ReadAll(response.Body)
	if err != nil {
		return "", v1alpha1.AnalysisPhaseError, nil, &responseReadError{err: err}
	}

	if response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusUnauthorized {
		return "", v1alpha1.AnalysisPhaseError, nil, fmt.Errorf("received authentication error response code: %v %s", response.StatusCode, string(bodyBytes))
	} else if response.StatusCode != http.StatusOK {
		return "", v1alpha1.AnalysisPhaseError, nil, fmt.Errorf("received non 2xx response code: %v %s", response.StatusCode, string(bodyBytes))
	}

	var res datadogResponseV2
	err = json.Unmarshal(bodyBytes, &res)
	if err != nil {
		return "", v1alpha1.AnalysisPhaseError, nil, fmt.Errorf("Could not parse JSON body: %v", err)
	}

	// Handle an error returned by Datadog
	if res.Data.Errors != "" {
		return "", v1alpha1.AnalysisPhaseError, nil, fmt.Errorf("There were errors in your query: %v", res.Data.Errors)
	}

	values, groups, grouped, err := extractValues(res.Data.Attributes.Columns)
	if err != nil {
		return "", v1alpha1.AnalysisPhaseError, nil, err
	}

	// Handle an empty query result
	if len(values) == 0 {
		var nilFloat64 *float64
		status, evalErr := evaluate.EvaluateResult(nilFloat64, metric, p.logCtx)
		return "[]", status, nil, evalErr
	}

	// Dispatch on the response shape, not the value count. An ungrouped query
	// is always a scalar, so it keeps returning a single value and preserves
	// the existing `successCondition: result < X` style of expression. A
	// grouped (`by {tag}`) query is always a slice — even when it matches a
	// single group — so users can apply Expr functions like `max(result)`,
	// `mean(result)`, `all(result, # < X)` directly in the condition. This
	// mirrors the Prometheus provider, which keys off the response type rather
	// than the number of samples returned.
	if !grouped {
		status, evalErr := evaluate.EvaluateResult(values[0], metric, p.logCtx)
		return strconv.FormatFloat(values[0], 'f', -1, 64), status, nil, evalErr
	}

	status, evalErr := evaluate.EvaluateResult(values, metric, p.logCtx)

	// For grouped queries, surface the (name, value) pairs as JSON so
	// operators can map an outlier in `result` back to the entity that
	// produced it. JSON-encoded rather than CSV because Datadog tag values
	// can legally contain `,` and `=`. The breakdown is capped for
	// high-cardinality tags; see maxGroupsInMetadata.
	groupsJSON, truncated := groupsMetadata(groups)
	metadata := map[string]string{"groups": groupsJSON}
	if truncated {
		metadata["groups_truncated"] = "true"
	}
	return formatValueSlice(values), status, metadata, evalErr
}

func formatValueSlice(values []float64) string {
	parts := make([]string, 0, len(values))
	for _, v := range values {
		parts = append(parts, strconv.FormatFloat(v, 'f', -1, 64))
	}
	return "[" + strings.Join(parts, ",") + "]"
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

	// If a request timeout is provided, it must be a valid, strictly positive duration.
	if dd.RequestTimeout != "" {
		timeout, err := dd.RequestTimeout.Duration()
		if err != nil {
			return fmt.Errorf("Could not parse the request timeout: %v. Please review the Analysis Template.", err)
		}
		if timeout <= 0 {
			return errors.New("Request timeout must be a positive duration (e.g. 30s). Please review the Analysis Template.")
		}
	}

	return nil
}

func NewDatadogProvider(logCtx log.Entry, kubeclientset kubernetes.Interface, namespace string, metric v1alpha1.Metric) (*Provider, error) {
	address, apiKey, appKey, err := findCredentials(logCtx, kubeclientset, namespace, metric)
	if err != nil {
		return nil, err
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

func findCredentials(logCtx log.Entry, kubeclientset kubernetes.Interface, namespace string, metric v1alpha1.Metric) (string, string, string, error) {
	finders := []CredentialsFinder{}
	secretName := metric.Provider.Datadog.SecretRef.Name
	namespaced := metric.Provider.Datadog.SecretRef.Namespaced
	credentialsNs := defaults.Namespace()

	if namespaced {
		credentialsNs = namespace
		if secretName == "" {
			return "", "", "", errors.New("secret name is required for namespaced credentials")
		}
	}

	if secretName != "" {
		finders = append(finders, NewSecretFinder(kubeclientset, secretName, credentialsNs))
	} else {
		finders = append(finders, NewEnvVariablesFinder(), NewSecretFinder(kubeclientset, DatadogTokensSecretName, defaults.Namespace()))
	}
	for _, finder := range finders {
		address, apiKey, appKey := finder.FindCredentials(logCtx)
		if apiKey != "" && appKey != "" {
			return address, apiKey, appKey, nil
		}
	}

	return "", "", "", errors.New("failed to find the credentials for datadog metrics provider")
}
