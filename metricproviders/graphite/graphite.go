package graphite

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"math"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
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

type graphiteDataPoint struct {
	Value     *float64
	TimeStamp time.Time
}

func (gdp *graphiteDataPoint) UnmarshalJSON(data []byte) error {
	var v []interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}

	if len(v) != 2 {
		return fmt.Errorf("error unmarshaling data point: %v", v)
	}

	switch v[0].(type) {
	case nil:
		// no value
	case float64:
		f, _ := v[0].(float64)
		gdp.Value = &f
	case string:
		f, err := strconv.ParseFloat(v[0].(string), 64)
		if err != nil {
			return err
		}
		gdp.Value = &f
	default:
		f, ok := v[0].(float64)
		if !ok {
			return fmt.Errorf("error unmarshaling value: %v", v[0])
		}
		gdp.Value = &f
	}

	switch v[1].(type) {
	case nil:
		// no value
	case float64:
		ts := int64(math.Round(v[1].(float64)))
		gdp.TimeStamp = time.Unix(ts, 0)
	case string:
		ts, err := strconv.ParseInt(v[1].(string), 10, 64)
		if err != nil {
			return err
		}
		gdp.TimeStamp = time.Unix(ts, 0)
	default:
		ts, ok := v[1].(int64)
		if !ok {
			return fmt.Errorf("error unmarshaling timestamp: %v", v[0])
		}
		gdp.TimeStamp = time.Unix(ts, 0)
	}

	return nil
}

type graphiteTargetResp struct {
	Target     string              `json:"target"`
	DataPoints []graphiteDataPoint `json:"datapoints"`
}

type graphiteResponse []graphiteTargetResp

// Provider contains the required components to run a Graphite query.
// TODO: add support for username/password authentication.
type Provider struct {
	url     url.URL
	client  *http.Client
	timeout time.Duration
	logCtx  log.Entry
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

	query := p.trimQuery(metric.Provider.Graphite.Query)
	u, err := url.Parse(fmt.Sprintf("./render?%s", query))
	if err != nil {
		return metricutil.MarkMeasurementError(newMeasurement, err)
	}

	q := u.Query()
	q.Set("format", "json")
	u.RawQuery = q.Encode()

	u.Path = path.Join(p.url.Path, u.Path)
	u = p.url.ResolveReference(u)

	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return metricutil.MarkMeasurementError(newMeasurement, err)
	}

	// TODO: make timeout configurable
	ctx, cancel := context.WithTimeout(req.Context(), p.timeout)
	defer cancel()

	r, err := p.client.Do(req.WithContext(ctx))
	if err != nil {
		return metricutil.MarkMeasurementError(newMeasurement, err)
	}
	defer r.Body.Close()

	b, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return metricutil.MarkMeasurementError(newMeasurement, err)
	}

	if 400 <= r.StatusCode {
		return metricutil.MarkMeasurementError(newMeasurement, err)
	}

	var result graphiteResponse
	err = json.Unmarshal(b, &result)
	if err != nil {
		return metricutil.MarkMeasurementError(newMeasurement, err)
	}

	var value *float64
	for _, tr := range result {
		for _, dp := range tr.DataPoints {
			if dp.Value != nil {
				value = dp.Value
			}
		}
	}

	if value == nil {
		return metricutil.MarkMeasurementError(newMeasurement, errors.New("no values found"))
	}

	newMeasurement.Value = fmt.Sprintf("%f", *value)

	newStatus, err := evaluate.EvaluateResult(value, metric, p.logCtx)
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

func (p *Provider) trimQuery(q string) string {
	space := regexp.MustCompile(`\s+`)
	return space.ReplaceAllString(q, " ")
}

// NewGraphiteProvider returns a new Graphite provider
func NewGraphiteProvider(metric v1alpha1.Metric, logCtx log.Entry) (*Provider, error) {
	addr := metric.Provider.Graphite.Address
	graphiteURL, err := url.Parse(addr)
	if addr == "" || err != nil {
		return nil, fmt.Errorf("%s address %s is not a valid URL", ProviderType, addr)
	}

	return &Provider{
		logCtx:  logCtx,
		client:  http.DefaultClient,
		url:     *graphiteURL,
		timeout: 5 * time.Second,
	}, nil
}
