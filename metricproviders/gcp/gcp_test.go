package gcp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/monitoring/apiv3/v2/monitoringpb"
	"github.com/googleapis/gax-go/v2/apierror"
	"github.com/prometheus/common/model"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	distributionpb "google.golang.org/genproto/googleapis/api/distribution"
	rpcstatus "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

type fakeRT struct {
	body   string
	status int
}

func (f *fakeRT) RoundTrip(r *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: f.status,
		Body:       io.NopCloser(strings.NewReader(f.body)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Request:    r,
	}, nil
}

func newAnalysisRun() *v1alpha1.AnalysisRun {
	return &v1alpha1.AnalysisRun{}
}

func TestType(t *testing.T) {
	e := log.Entry{}
	p := NewGCPProvider(&mockAPI{}, e)
	assert.Equal(t, ProviderType, p.Type())
}

func TestRunSuccessfulPromQL(t *testing.T) {
	mock := &mockAPI{promResponse: [][]float64{{0.1, 0.2, 0.3}}}
	p := NewGCPProvider(mock, log.Entry{})
	metric := v1alpha1.Metric{
		Name:             "foo",
		SuccessCondition: "result[0][0] == 0.1",
		FailureCondition: "result[0][0] != 0.1",
		Provider: v1alpha1.MetricProvider{
			GCP: &v1alpha1.GCPMetric{
				Project:  "my-project",
				Interval: "10m",
				Query:    `up{job="foo"}`,
			},
		},
	}
	measurement := p.Run(newAnalysisRun(), metric)
	assert.NotNil(t, measurement.StartedAt)
	assert.Equal(t, "[[0.1,0.2,0.3]]", measurement.Value)
	assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, measurement.Phase)
	assert.Equal(t, "my-project", mock.lastProject)
	assert.Equal(t, `up{job="foo"}`, mock.lastQuery)
	assert.Equal(t, 10*time.Minute, mock.lastInterval)
}

func TestRunSuccessfulFilter(t *testing.T) {
	mock := &mockAPI{timeSeriesResponse: [][]float64{{1.5}}}
	p := NewGCPProvider(mock, log.Entry{})
	metric := v1alpha1.Metric{
		Name:             "foo",
		SuccessCondition: "result[0][0] == 1.5",
		Provider: v1alpha1.MetricProvider{
			GCP: &v1alpha1.GCPMetric{
				Project: "my-project",
				Filter:  `metric.type="compute.googleapis.com/instance/cpu/utilization"`,
				Aggregation: &v1alpha1.GCPAggregation{
					AlignmentPeriod:  "60s",
					PerSeriesAligner: "ALIGN_MEAN",
				},
			},
		},
	}
	measurement := p.Run(newAnalysisRun(), metric)
	assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, measurement.Phase)
	assert.Equal(t, "[[1.5]]", measurement.Value)
	assert.Equal(t, `metric.type="compute.googleapis.com/instance/cpu/utilization"`, mock.lastFilter)
	assert.Equal(t, 5*time.Minute, mock.lastInterval)
}

func TestRunErrorsWhenMissingProject(t *testing.T) {
	p := NewGCPProvider(&mockAPI{}, log.Entry{})
	metric := v1alpha1.Metric{
		Provider: v1alpha1.MetricProvider{
			GCP: &v1alpha1.GCPMetric{Query: "up"},
		},
	}
	measurement := p.Run(newAnalysisRun(), metric)
	assert.Equal(t, v1alpha1.AnalysisPhaseError, measurement.Phase)
	assert.Contains(t, measurement.Message, "gcp.project is required")
}

func TestRunErrorsWhenBothQueryAndFilterSet(t *testing.T) {
	p := NewGCPProvider(&mockAPI{}, log.Entry{})
	metric := v1alpha1.Metric{
		Provider: v1alpha1.MetricProvider{
			GCP: &v1alpha1.GCPMetric{
				Project: "p",
				Query:   "up",
				Filter:  "metric.type=\"x\"",
			},
		},
	}
	measurement := p.Run(newAnalysisRun(), metric)
	assert.Equal(t, v1alpha1.AnalysisPhaseError, measurement.Phase)
	assert.Contains(t, measurement.Message, "only one of")
}

func TestRunErrorsWhenNeitherQueryNorFilterSet(t *testing.T) {
	p := NewGCPProvider(&mockAPI{}, log.Entry{})
	metric := v1alpha1.Metric{
		Provider: v1alpha1.MetricProvider{
			GCP: &v1alpha1.GCPMetric{Project: "p"},
		},
	}
	measurement := p.Run(newAnalysisRun(), metric)
	assert.Equal(t, v1alpha1.AnalysisPhaseError, measurement.Phase)
	assert.Contains(t, measurement.Message, "one of gcp.query")
}

func TestRunErrorsOnQueryFailure(t *testing.T) {
	wantErr := fmt.Errorf("network unreachable")
	mock := &mockAPI{err: wantErr}
	p := NewGCPProvider(mock, log.Entry{})
	metric := v1alpha1.Metric{
		Provider: v1alpha1.MetricProvider{
			GCP: &v1alpha1.GCPMetric{Project: "p", Query: "up"},
		},
	}
	measurement := p.Run(newAnalysisRun(), metric)
	assert.Equal(t, v1alpha1.AnalysisPhaseError, measurement.Phase)
	assert.Equal(t, wantErr.Error(), measurement.Message)
}

func TestRunErrorsWhenGCPProviderNil(t *testing.T) {
	p := NewGCPProvider(&mockAPI{}, log.Entry{})
	measurement := p.Run(newAnalysisRun(), v1alpha1.Metric{})
	assert.Equal(t, v1alpha1.AnalysisPhaseError, measurement.Phase)
	assert.Contains(t, measurement.Message, "gcp provider not set")
}

func TestRunErrorsOnEvaluateFailure(t *testing.T) {
	mock := &mockAPI{promResponse: [][]float64{{1}}}
	p := NewGCPProvider(mock, log.Entry{})
	metric := v1alpha1.Metric{
		Name:             "foo",
		SuccessCondition: "this is not a valid expr expression !!!",
		Provider: v1alpha1.MetricProvider{
			GCP: &v1alpha1.GCPMetric{Project: "p", Query: "up"},
		},
	}
	measurement := p.Run(newAnalysisRun(), metric)
	assert.Equal(t, v1alpha1.AnalysisPhaseError, measurement.Phase)
	assert.NotEmpty(t, measurement.Message)
}

func TestRunRespectsQueryTimeout(t *testing.T) {
	mock := &mockAPI{block: true}
	p := NewGCPProvider(mock, log.Entry{})
	metric := v1alpha1.Metric{
		Provider: v1alpha1.MetricProvider{
			GCP: &v1alpha1.GCPMetric{Project: "p", Query: "up", Timeout: ptr.To(int64(0))},
		},
	}
	measurement := p.Run(newAnalysisRun(), metric)
	assert.Equal(t, v1alpha1.AnalysisPhaseError, measurement.Phase)
	assert.Contains(t, measurement.Message, "context deadline exceeded")
}

func TestRunErrorsOnNegativeTimeout(t *testing.T) {
	p := NewGCPProvider(&mockAPI{}, log.Entry{})
	metric := v1alpha1.Metric{
		Provider: v1alpha1.MetricProvider{
			GCP: &v1alpha1.GCPMetric{Project: "p", Query: "up", Timeout: ptr.To(int64(-1))},
		},
	}
	measurement := p.Run(newAnalysisRun(), metric)
	assert.Equal(t, v1alpha1.AnalysisPhaseError, measurement.Phase)
	assert.Contains(t, measurement.Message, "gcp.timeout must not be negative")
}

func TestRunErrorsOnInvalidInterval(t *testing.T) {
	p := NewGCPProvider(&mockAPI{}, log.Entry{})
	metric := v1alpha1.Metric{
		Provider: v1alpha1.MetricProvider{
			GCP: &v1alpha1.GCPMetric{Project: "p", Query: "up", Interval: "not-a-duration"},
		},
	}
	measurement := p.Run(newAnalysisRun(), metric)
	assert.Equal(t, v1alpha1.AnalysisPhaseError, measurement.Phase)
	assert.Contains(t, measurement.Message, "invalid gcp.interval")
}

func TestResume(t *testing.T) {
	p := NewGCPProvider(&mockAPI{}, log.Entry{})
	now := metav1.Now()
	prev := v1alpha1.Measurement{StartedAt: &now, Phase: v1alpha1.AnalysisPhaseInconclusive}
	assert.Equal(t, prev, p.Resume(newAnalysisRun(), v1alpha1.Metric{}, prev))
}

func TestTerminate(t *testing.T) {
	p := NewGCPProvider(&mockAPI{}, log.Entry{})
	now := metav1.Now()
	prev := v1alpha1.Measurement{StartedAt: &now, Phase: v1alpha1.AnalysisPhaseRunning}
	assert.Equal(t, prev, p.Terminate(newAnalysisRun(), v1alpha1.Metric{}, prev))
}

func TestGarbageCollect(t *testing.T) {
	p := NewGCPProvider(&mockAPI{}, log.Entry{})
	assert.NoError(t, p.GarbageCollect(nil, v1alpha1.Metric{}, 0))
}

func TestGetMetadata(t *testing.T) {
	p := NewGCPProvider(&mockAPI{}, log.Entry{})

	t.Run("nil provider returns nil", func(t *testing.T) {
		assert.Nil(t, p.GetMetadata(v1alpha1.Metric{}))
	})

	t.Run("query mode surfaces ResolvedGCPQuery only", func(t *testing.T) {
		metric := v1alpha1.Metric{
			Provider: v1alpha1.MetricProvider{
				GCP: &v1alpha1.GCPMetric{Project: "p", Query: `up{job="foo"}`},
			},
		}
		md := p.GetMetadata(metric)
		assert.Equal(t, `up{job="foo"}`, md[ResolvedGCPQuery])
		_, hasFilter := md[ResolvedGCPFilter]
		assert.False(t, hasFilter)
	})

	t.Run("filter mode surfaces ResolvedGCPFilter only", func(t *testing.T) {
		metric := v1alpha1.Metric{
			Provider: v1alpha1.MetricProvider{
				GCP: &v1alpha1.GCPMetric{Project: "p", Filter: `metric.type="x"`},
			},
		}
		md := p.GetMetadata(metric)
		assert.Equal(t, `metric.type="x"`, md[ResolvedGCPFilter])
		_, hasQuery := md[ResolvedGCPQuery]
		assert.False(t, hasQuery)
	})
}

func TestPromValueToSeries(t *testing.T) {
	t.Run("vector", func(t *testing.T) {
		v := model.Vector{
			&model.Sample{Value: 1.5},
			&model.Sample{Value: 2.5},
		}
		got, err := promValueToSeries(v)
		assert.NoError(t, err)
		assert.Equal(t, [][]float64{{1.5}, {2.5}}, got)
	})
	t.Run("matrix", func(t *testing.T) {
		v := model.Matrix{
			&model.SampleStream{Values: []model.SamplePair{{Value: 1}, {Value: 2}}},
		}
		got, err := promValueToSeries(v)
		assert.NoError(t, err)
		assert.Equal(t, [][]float64{{1, 2}}, got)
	})
	t.Run("scalar", func(t *testing.T) {
		v := &model.Scalar{Value: 7}
		got, err := promValueToSeries(v)
		assert.NoError(t, err)
		assert.Equal(t, [][]float64{{7}}, got)
	})
	t.Run("unsupported", func(t *testing.T) {
		_, err := promValueToSeries(&model.String{Value: "bad"})
		assert.Error(t, err)
	})
}

func TestBuildAggregation(t *testing.T) {
	t.Run("nil returns nil", func(t *testing.T) {
		out, err := buildAggregation(nil)
		assert.NoError(t, err)
		assert.Nil(t, out)
	})
	t.Run("happy path", func(t *testing.T) {
		out, err := buildAggregation(&v1alpha1.GCPAggregation{
			AlignmentPeriod:    "60s",
			PerSeriesAligner:   "ALIGN_MEAN",
			CrossSeriesReducer: "REDUCE_MEAN",
			GroupByFields:      []string{"resource.label.zone"},
		})
		assert.NoError(t, err)
		assert.NotNil(t, out)
		assert.Equal(t, monitoringpb.Aggregation_ALIGN_MEAN, out.PerSeriesAligner)
		assert.Equal(t, monitoringpb.Aggregation_REDUCE_MEAN, out.CrossSeriesReducer)
		assert.Equal(t, []string{"resource.label.zone"}, out.GroupByFields)
		assert.Equal(t, int64(60), out.AlignmentPeriod.Seconds)
	})
	t.Run("bad aligner", func(t *testing.T) {
		_, err := buildAggregation(&v1alpha1.GCPAggregation{PerSeriesAligner: "BOGUS"})
		assert.Error(t, err)
	})
	t.Run("bad reducer", func(t *testing.T) {
		_, err := buildAggregation(&v1alpha1.GCPAggregation{CrossSeriesReducer: "BOGUS"})
		assert.Error(t, err)
	})
	t.Run("bad alignment period", func(t *testing.T) {
		_, err := buildAggregation(&v1alpha1.GCPAggregation{AlignmentPeriod: "abc"})
		assert.Error(t, err)
	})
}

func TestPointToFloat(t *testing.T) {
	cases := []struct {
		name string
		in   *monitoringpb.TypedValue
		want float64
	}{
		{"double", &monitoringpb.TypedValue{Value: &monitoringpb.TypedValue_DoubleValue{DoubleValue: 1.5}}, 1.5},
		{"int64", &monitoringpb.TypedValue{Value: &monitoringpb.TypedValue_Int64Value{Int64Value: 7}}, 7},
		{"bool true", &monitoringpb.TypedValue{Value: &monitoringpb.TypedValue_BoolValue{BoolValue: true}}, 1},
		{"bool false", &monitoringpb.TypedValue{Value: &monitoringpb.TypedValue_BoolValue{BoolValue: false}}, 0},
		{"distribution mean", &monitoringpb.TypedValue{Value: &monitoringpb.TypedValue_DistributionValue{DistributionValue: &distributionpb.Distribution{Count: 3, Mean: 4.2}}}, 4.2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := pointToFloat(tc.in)
			assert.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}

	t.Run("nil distribution errors", func(t *testing.T) {
		v := &monitoringpb.TypedValue{Value: &monitoringpb.TypedValue_DistributionValue{DistributionValue: nil}}
		_, err := pointToFloat(v)
		assert.ErrorContains(t, err, "empty distribution")
	})

	t.Run("zero-count distribution errors", func(t *testing.T) {
		v := &monitoringpb.TypedValue{Value: &monitoringpb.TypedValue_DistributionValue{DistributionValue: &distributionpb.Distribution{Count: 0}}}
		_, err := pointToFloat(v)
		assert.ErrorContains(t, err, "empty distribution")
	})

	t.Run("nil typed value errors", func(t *testing.T) {
		_, err := pointToFloat(&monitoringpb.TypedValue{})
		assert.ErrorContains(t, err, "unsupported typed value")
	})

	t.Run("string typed value errors", func(t *testing.T) {
		v := &monitoringpb.TypedValue{Value: &monitoringpb.TypedValue_StringValue{StringValue: "oops"}}
		_, err := pointToFloat(v)
		assert.ErrorContains(t, err, "unsupported typed value")
	})
}

func TestConvertTimeSeries(t *testing.T) {
	double := func(v float64) *monitoringpb.TypedValue {
		return &monitoringpb.TypedValue{Value: &monitoringpb.TypedValue_DoubleValue{DoubleValue: v}}
	}
	t.Run("empty input returns nil", func(t *testing.T) {
		got, err := convertTimeSeries(nil)
		assert.NoError(t, err)
		assert.Nil(t, got)
	})
	t.Run("multiple series with multiple points", func(t *testing.T) {
		items := []*monitoringpb.TimeSeries{
			{Points: []*monitoringpb.Point{{Value: double(0.1)}, {Value: double(0.2)}}},
			{Points: []*monitoringpb.Point{{Value: double(0.5)}}},
		}
		got, err := convertTimeSeries(items)
		require.NoError(t, err)
		assert.Equal(t, [][]float64{{0.1, 0.2}, {0.5}}, got)
	})
	t.Run("point decode error propagates", func(t *testing.T) {
		bad := &monitoringpb.TypedValue{Value: &monitoringpb.TypedValue_DistributionValue{DistributionValue: nil}}
		items := []*monitoringpb.TimeSeries{
			{Points: []*monitoringpb.Point{{Value: double(1)}, {Value: bad}}},
		}
		_, err := convertTimeSeries(items)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "decode time series point")
	})
}

func TestExtractExecutionErrors(t *testing.T) {
	t.Run("empty input returns nil", func(t *testing.T) {
		assert.Nil(t, extractExecutionErrors(nil))
	})
	t.Run("collects messages", func(t *testing.T) {
		errs := []*rpcstatus.Status{
			{Message: "quota exceeded"},
			{Message: "metric not found"},
		}
		got := extractExecutionErrors(errs)
		assert.Equal(t, []string{"quota exceeded", "metric not found"}, got)
	})
}

func TestRunMergesProviderMetadata(t *testing.T) {
	mock := &mockAPI{
		promResponse: [][]float64{{1}},
		metadata:     map[string]string{"warnings": `"limit reached"`},
	}
	p := NewGCPProvider(mock, log.Entry{})
	metric := v1alpha1.Metric{
		Name:             "foo",
		SuccessCondition: "result[0][0] == 1",
		Provider: v1alpha1.MetricProvider{
			GCP: &v1alpha1.GCPMetric{Project: "p", Query: "up"},
		},
	}
	measurement := p.Run(newAnalysisRun(), metric)
	assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, measurement.Phase)
	assert.Equal(t, `"limit reached"`, measurement.Metadata["warnings"])
}

func TestFormatSeries(t *testing.T) {
	cases := []struct {
		name string
		in   [][]float64
		want string
	}{
		{"empty", [][]float64{}, "[]"},
		{"single point", [][]float64{{1.5}}, "[[1.5]]"},
		{"multi point", [][]float64{{0.1, 0.2, 0.3}}, "[[0.1,0.2,0.3]]"},
		{"multi series", [][]float64{{1, 2}, {3}}, "[[1,2],[3]]"},
		{"NaN tolerated", [][]float64{{math.NaN(), 0.5}}, "[[NaN,0.5]]"},
		{"+Inf tolerated", [][]float64{{math.Inf(1)}}, "[[+Inf]]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, formatSeries(tc.in))
		})
	}
}

func TestWarningsToMetadata(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want map[string]string
	}{
		{"empty", nil, nil},
		{"single", []string{"deprecated metric"}, map[string]string{"warnings": `"deprecated metric"`}},
		{"multiple", []string{"a", "b"}, map[string]string{"warnings": `"a", "b"`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, warningsToMetadata(tc.in))
		})
	}
}

func TestPromStep(t *testing.T) {
	assert.Equal(t, time.Second, promStep(10*time.Second))
	assert.Equal(t, 5*time.Second, promStep(5*time.Minute))
	assert.Equal(t, 10*time.Second, promStep(10*time.Minute))
}

func TestAsGoogleAPIError(t *testing.T) {
	t.Run("plain error passes through", func(t *testing.T) {
		want := errors.New("boom")
		assert.Equal(t, want, asGoogleAPIError(want))
	})
	t.Run("wrapped APIError is unwrapped to its message", func(t *testing.T) {
		st := status.New(codes.PermissionDenied, "missing monitoring.viewer")
		ae, ok := apierror.FromError(st.Err())
		require.True(t, ok)
		got := asGoogleAPIError(fmt.Errorf("outer: %w", ae))
		assert.NotNil(t, got)
		assert.Contains(t, got.Error(), "missing monitoring.viewer")
	})
}

func TestGCPClientQueryPromQL(t *testing.T) {
	const matrixResponse = `{
		"status": "success",
		"data": {
			"resultType": "matrix",
			"result": [{
				"metric": {"__name__": "rate"},
				"values": [[1700000000, "0.1"], [1700000060, "0.2"]]
			}]
		},
		"warnings": ["sampled"]
	}`
	c := &gcpClient{httpClient: &http.Client{Transport: &fakeRT{body: matrixResponse, status: 200}}}

	series, meta, err := c.QueryPromQL(context.Background(), "p", `rate(x[1m])`, time.Minute)
	require.NoError(t, err)
	assert.Equal(t, [][]float64{{0.1, 0.2}}, series)
	assert.Equal(t, map[string]string{"warnings": `"sampled"`}, meta)
}

func TestNewGCPAPIClientErrorsWithoutCreds(t *testing.T) {
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "/nonexistent/keyfile.json")
	_, err := NewGCPAPIClient(v1alpha1.Metric{})
	require.Error(t, err)
}

func TestGCPClientQueryPromQLErrorPropagates(t *testing.T) {
	c := &gcpClient{httpClient: &http.Client{Transport: &fakeRT{body: "bad gateway", status: http.StatusBadGateway}}}
	_, _, err := c.QueryPromQL(context.Background(), "p", "up", time.Minute)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "gcp: promql query failed")
}

func TestGCPClientQueryTimeSeriesAggregationError(t *testing.T) {
	// nil metricClient is fine — buildAggregation errors before we'd reach it.
	c := &gcpClient{}
	q := &v1alpha1.GCPMetric{
		Project: "p",
		Filter:  `metric.type="x"`,
		Aggregation: &v1alpha1.GCPAggregation{
			PerSeriesAligner: "BOGUS_ALIGNER",
		},
	}
	_, _, err := c.QueryTimeSeries(context.Background(), "p", q, time.Minute)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown perSeriesAligner")
}
