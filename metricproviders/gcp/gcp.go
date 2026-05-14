package gcp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	monitoring "cloud.google.com/go/monitoring/apiv3/v2"
	"cloud.google.com/go/monitoring/apiv3/v2/monitoringpb"
	"github.com/googleapis/gax-go/v2/apierror"
	promapi "github.com/prometheus/client_golang/api"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	log "github.com/sirupsen/logrus"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
	rpcstatus "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/evaluate"
	metricutil "github.com/argoproj/argo-rollouts/utils/metric"
	timeutil "github.com/argoproj/argo-rollouts/utils/time"
)

const (
	// ProviderType indicates the provider is GCP (Cloud Monitoring).
	ProviderType = "GCP"

	monitoringReadScope = "https://www.googleapis.com/auth/monitoring.read"
	promQLEndpointFmt   = "https://monitoring.googleapis.com/v1/projects/%s/location/global/prometheus"

	defaultInterval     = 5 * time.Minute
	defaultQueryTimeout = 30 * time.Second
	promQLTargetPoints  = 60

	// ResolvedGCPQuery is the key used to store the resolved PromQL query
	// on the measurement metadata.
	ResolvedGCPQuery = "ResolvedGCPQuery"
	// ResolvedGCPFilter is the key used to store the resolved ListTimeSeries
	// filter on the measurement metadata.
	ResolvedGCPFilter = "ResolvedGCPFilter"
)

// GCPClientAPI abstracts the two query paths supported by this provider.
type GCPClientAPI interface {
	QueryPromQL(ctx context.Context, project, query string, interval time.Duration) ([][]float64, map[string]string, error)
	QueryTimeSeries(ctx context.Context, project string, query *v1alpha1.GCPMetric, interval time.Duration) ([][]float64, map[string]string, error)
}

// Provider contains all the required components to run a GCP query.
// Implements the Provider Interface.
type Provider struct {
	api    GCPClientAPI
	logCtx log.Entry
}

// Type indicates provider is a GCP provider.
func (p *Provider) Type() string {
	return ProviderType
}

// GetMetadata returns the resolved query or filter so it shows up on the measurement.
func (p *Provider) GetMetadata(metric v1alpha1.Metric) map[string]string {
	if metric.Provider.GCP == nil {
		return nil
	}
	md := map[string]string{}
	if metric.Provider.GCP.Query != "" {
		md[ResolvedGCPQuery] = metric.Provider.GCP.Query
	}
	if metric.Provider.GCP.Filter != "" {
		md[ResolvedGCPFilter] = metric.Provider.GCP.Filter
	}
	return md
}

// Run queries GCP Cloud Monitoring for the metric.
func (p *Provider) Run(run *v1alpha1.AnalysisRun, m v1alpha1.Metric) v1alpha1.Measurement {
	startTime := timeutil.MetaNow()
	measurement := v1alpha1.Measurement{
		StartedAt: &startTime,
		Metadata:  map[string]string{},
	}

	gcp := m.Provider.GCP
	if gcp == nil {
		return metricutil.MarkMeasurementError(measurement, errors.New("gcp provider not set"))
	}
	if gcp.Project == "" {
		return metricutil.MarkMeasurementError(measurement, errors.New("gcp.project is required"))
	}
	if gcp.Query == "" && gcp.Filter == "" {
		return metricutil.MarkMeasurementError(measurement, errors.New("one of gcp.query (PromQL) or gcp.filter (ListTimeSeries) must be set"))
	}
	if gcp.Query != "" && gcp.Filter != "" {
		return metricutil.MarkMeasurementError(measurement, errors.New("only one of gcp.query or gcp.filter may be set"))
	}

	interval := defaultInterval
	if gcp.Interval != "" {
		d, err := gcp.Interval.Duration()
		if err != nil {
			return metricutil.MarkMeasurementError(measurement, fmt.Errorf("invalid gcp.interval: %w", err))
		}
		interval = d
	}

	timeout := defaultQueryTimeout
	if gcp.Timeout != nil {
		if *gcp.Timeout < 0 {
			return metricutil.MarkMeasurementError(measurement, errors.New("gcp.timeout must not be negative"))
		}
		timeout = time.Duration(*gcp.Timeout) * time.Second
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var (
		series [][]float64
		meta   map[string]string
		err    error
	)
	if gcp.Query != "" {
		series, meta, err = p.api.QueryPromQL(ctx, gcp.Project, gcp.Query, interval)
	} else {
		series, meta, err = p.api.QueryTimeSeries(ctx, gcp.Project, gcp, interval)
	}
	if err != nil {
		return metricutil.MarkMeasurementError(measurement, err)
	}

	for k, v := range meta {
		measurement.Metadata[k] = v
	}

	measurement.Value = formatSeries(series)

	status, err := evaluate.EvaluateResult(series, m, p.logCtx)
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

// NewGCPProvider creates a new GCP provider.
func NewGCPProvider(api GCPClientAPI, logCtx log.Entry) *Provider {
	return &Provider{api: api, logCtx: logCtx}
}

// NewGCPAPIClient generates a GCP API client backed by Application Default Credentials.
func NewGCPAPIClient(_ v1alpha1.Metric) (GCPClientAPI, error) {
	ctx := context.Background()
	httpClient, err := google.DefaultClient(ctx, monitoringReadScope)
	if err != nil {
		return nil, fmt.Errorf("gcp: build default credentials client: %w", err)
	}
	metricClient, err := monitoring.NewMetricClient(ctx, option.WithScopes(monitoringReadScope))
	if err != nil {
		return nil, fmt.Errorf("gcp: build monitoring client: %w", err)
	}
	return &gcpClient{httpClient: httpClient, metricClient: metricClient}, nil
}

type gcpClient struct {
	httpClient   *http.Client
	metricClient *monitoring.MetricClient
}

func (c *gcpClient) QueryPromQL(ctx context.Context, project, query string, interval time.Duration) ([][]float64, map[string]string, error) {
	api, err := newPromAPI(c.httpClient, project)
	if err != nil {
		return nil, nil, err
	}
	end := timeutil.Now()
	start := end.Add(-interval)
	val, warnings, err := api.QueryRange(ctx, query, promv1.Range{
		Start: start,
		End:   end,
		Step:  promStep(interval),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("gcp: promql query failed: %w", err)
	}
	for _, w := range warnings {
		log.WithField("provider", ProviderType).Warnf("gcp promql warning: %s", w)
	}
	series, err := promValueToSeries(val)
	if err != nil {
		return nil, nil, err
	}
	return series, warningsToMetadata([]string(warnings)), nil
}

func (c *gcpClient) QueryTimeSeries(ctx context.Context, project string, q *v1alpha1.GCPMetric, interval time.Duration) ([][]float64, map[string]string, error) {
	end := timeutil.Now()
	start := end.Add(-interval)
	req := &monitoringpb.ListTimeSeriesRequest{
		Name:   "projects/" + project,
		Filter: q.Filter,
		Interval: &monitoringpb.TimeInterval{
			StartTime: timestamppb.New(start),
			EndTime:   timestamppb.New(end),
		},
		View: monitoringpb.ListTimeSeriesRequest_FULL,
	}
	agg, err := buildAggregation(q.Aggregation)
	if err != nil {
		return nil, nil, err
	}
	if agg != nil {
		req.Aggregation = agg
	}

	it := c.metricClient.ListTimeSeries(ctx, req)
	var items []*monitoringpb.TimeSeries
	for {
		ts, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("gcp: list time series: %w", asGoogleAPIError(err))
		}
		items = append(items, ts)
	}
	series, err := convertTimeSeries(items)
	if err != nil {
		return nil, nil, err
	}
	var execErrors []string
	if resp, ok := it.Response.(*monitoringpb.ListTimeSeriesResponse); ok {
		execErrors = extractExecutionErrors(resp.GetExecutionErrors())
	}
	return series, warningsToMetadata(execErrors), nil
}

func convertTimeSeries(items []*monitoringpb.TimeSeries) ([][]float64, error) {
	if len(items) == 0 {
		return nil, nil
	}
	series := make([][]float64, 0, len(items))
	for _, ts := range items {
		values := make([]float64, 0, len(ts.Points))
		for _, p := range ts.Points {
			v, err := pointToFloat(p.Value)
			if err != nil {
				return nil, fmt.Errorf("gcp: decode time series point: %w", err)
			}
			values = append(values, v)
		}
		series = append(series, values)
	}
	return series, nil
}

func extractExecutionErrors(errs []*rpcstatus.Status) []string {
	if len(errs) == 0 {
		return nil
	}
	msgs := make([]string, 0, len(errs))
	for _, s := range errs {
		log.WithField("provider", ProviderType).Warnf("gcp list time series execution error: %s", s.GetMessage())
		msgs = append(msgs, s.GetMessage())
	}
	return msgs
}

func newPromAPI(httpClient *http.Client, project string) (promv1.API, error) {
	client, err := promapi.NewClient(promapi.Config{
		Address: fmt.Sprintf(promQLEndpointFmt, project),
		Client:  httpClient,
	})
	if err != nil {
		return nil, fmt.Errorf("gcp: build prometheus client: %w", err)
	}
	return promv1.NewAPI(client), nil
}

func promStep(interval time.Duration) time.Duration {
	step := interval / promQLTargetPoints
	if step < time.Second {
		return time.Second
	}
	return step
}

func promValueToSeries(v model.Value) ([][]float64, error) {
	switch val := v.(type) {
	case model.Matrix:
		out := make([][]float64, 0, len(val))
		for _, s := range val {
			values := make([]float64, 0, len(s.Values))
			for _, sample := range s.Values {
				values = append(values, float64(sample.Value))
			}
			out = append(out, values)
		}
		return out, nil
	case model.Vector:
		out := make([][]float64, 0, len(val))
		for _, s := range val {
			out = append(out, []float64{float64(s.Value)})
		}
		return out, nil
	case *model.Scalar:
		return [][]float64{{float64(val.Value)}}, nil
	default:
		return nil, fmt.Errorf("gcp: unsupported promql result type %T", v)
	}
}

func buildAggregation(a *v1alpha1.GCPAggregation) (*monitoringpb.Aggregation, error) {
	if a == nil {
		return nil, nil
	}
	out := &monitoringpb.Aggregation{}
	if a.AlignmentPeriod != "" {
		d, err := a.AlignmentPeriod.Duration()
		if err != nil {
			return nil, fmt.Errorf("invalid aggregation.alignmentPeriod: %w", err)
		}
		out.AlignmentPeriod = durationpb.New(d)
	}
	if a.PerSeriesAligner != "" {
		aligner, ok := monitoringpb.Aggregation_Aligner_value[strings.ToUpper(a.PerSeriesAligner)]
		if !ok {
			return nil, fmt.Errorf("unknown perSeriesAligner %q (expected ALIGN_*)", a.PerSeriesAligner)
		}
		out.PerSeriesAligner = monitoringpb.Aggregation_Aligner(aligner)
	}
	if a.CrossSeriesReducer != "" {
		reducer, ok := monitoringpb.Aggregation_Reducer_value[strings.ToUpper(a.CrossSeriesReducer)]
		if !ok {
			return nil, fmt.Errorf("unknown crossSeriesReducer %q (expected REDUCE_*)", a.CrossSeriesReducer)
		}
		out.CrossSeriesReducer = monitoringpb.Aggregation_Reducer(reducer)
		out.GroupByFields = a.GroupByFields
	}
	return out, nil
}

func pointToFloat(v *monitoringpb.TypedValue) (float64, error) {
	switch x := v.GetValue().(type) {
	case *monitoringpb.TypedValue_DoubleValue:
		return x.DoubleValue, nil
	case *monitoringpb.TypedValue_Int64Value:
		return float64(x.Int64Value), nil
	case *monitoringpb.TypedValue_BoolValue:
		if x.BoolValue {
			return 1, nil
		}
		return 0, nil
	case *monitoringpb.TypedValue_DistributionValue:
		if x.DistributionValue == nil || x.DistributionValue.Count == 0 {
			return 0, fmt.Errorf("gcp: empty distribution")
		}
		return x.DistributionValue.Mean, nil
	default:
		return 0, fmt.Errorf("gcp: unsupported typed value: %T", v.GetValue())
	}
}

// formatSeries renders the result as a JSON-shaped string, e.g. [[0.1,0.2],[0.5]].
// NaN/±Inf are emitted as bare tokens.
func formatSeries(series [][]float64) string {
	inner := make([]string, len(series))
	for i, s := range series {
		vals := make([]string, len(s))
		for j, v := range s {
			vals[j] = strconv.FormatFloat(v, 'f', -1, 64)
		}
		inner[i] = "[" + strings.Join(vals, ",") + "]"
	}
	return "[" + strings.Join(inner, ",") + "]"
}

// warningsToMetadata renders a list of warning messages into a
// comma-separated quoted string under the "warnings" metadata key.
func warningsToMetadata(msgs []string) map[string]string {
	if len(msgs) == 0 {
		return nil
	}
	quoted := make([]string, len(msgs))
	for i, m := range msgs {
		quoted[i] = fmt.Sprintf("%q", m)
	}
	return map[string]string{"warnings": strings.Join(quoted, ", ")}
}

func asGoogleAPIError(err error) error {
	var ae *apierror.APIError
	if errors.As(err, &ae) {
		return errors.New(ae.Error())
	}
	return err
}
