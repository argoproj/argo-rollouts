package kayenta

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	metricutil "github.com/argoproj/argo-rollouts/utils/metric"
	timeutil "github.com/argoproj/argo-rollouts/utils/time"
)

const (
	// ProviderType indicates the provider is kayenta
	ProviderType   = "Kayenta"
	scoreURLFormat = `%s/canary/%s`

	configIdLookupURLFormat = `%s/canaryConfig?application=%s&configurationAccountName=%s`

	jobURLFormat = `%s/canary/%s?application=%s&metricsAccountName=%s&configurationAccountName=%s&storageAccountName=%s`

	resumeDelay           time.Duration = 15 * time.Second
	httpConnectionTimeout time.Duration = 15 * time.Second
)

type Provider struct {
	logCtx log.Entry
	client http.Client
}

type canaryConfig struct {
	Id                  string
	Name                string
	UpdatedTimestamp    int
	UpdatedTimestampIso string
	Applications        []string
}

type AnalysisRequest struct {
	Scopes     map[string]ScopeRequest `json:"scopes"`
	Thresholds ThresholdsRequest       `json:"thresholds"`
}

type ScopeRequest struct {
	ControlScope    v1alpha1.ScopeDetail `json:"controlScope"`
	ExperimentScope v1alpha1.ScopeDetail `json:"experimentScope"`
}

type ThresholdsRequest struct {
	Pass     int64 `json:"pass"`
	Marginal int64 `json:"marginal"`
}

// Type indicates provider is a kayenta provider
func (p *Provider) Type() string {
	return ProviderType
}

// GetMetadata returns any additional metadata which needs to be stored & displayed as part of the metrics result.
func (p *Provider) GetMetadata(metric v1alpha1.Metric) map[string]string {
	return nil
}

func getCanaryConfigId(metric v1alpha1.Metric, p *Provider) (string, error) {
	configIdLookupURL := fmt.Sprintf(configIdLookupURLFormat, metric.Provider.Kayenta.Address, metric.Provider.Kayenta.Application, metric.Provider.Kayenta.StorageAccountName)

	response, err := p.client.Get(configIdLookupURL)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()

	if response.StatusCode != 200 {
		return "", fmt.Errorf("Invalid Response: HTTP %d", response.StatusCode)
	}

	var cc []canaryConfig
	if err := json.NewDecoder(response.Body).Decode(&cc); err != nil {
		return "", err
	}

	for _, s := range cc {
		if s.Name == metric.Provider.Kayenta.CanaryConfigName {
			return s.Id, nil
		}
	}

	return "", errors.New("Canary config not found")
}

// Run queries kayentd for the metric
func (p *Provider) Run(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric) v1alpha1.Measurement {
	startTime := timeutil.MetaNow()
	newMeasurement := v1alpha1.Measurement{
		StartedAt: &startTime,
	}

	canaryConfigId, err := getCanaryConfigId(metric, p)
	if err != nil {
		return metricutil.MarkMeasurementError(newMeasurement, err)
	}

	jobURL := fmt.Sprintf(jobURLFormat, metric.Provider.Kayenta.Address, canaryConfigId, metric.Provider.Kayenta.Application, metric.Provider.Kayenta.MetricsAccountName, metric.Provider.Kayenta.ConfigurationAccountName, metric.Provider.Kayenta.StorageAccountName)

	scopes := make(map[string]ScopeRequest)
	for _, ks := range metric.Provider.Kayenta.Scopes {
		s, err := getScopeRequest(ks, run.Status.StartedAt, startTime, metric.Interval, metric.Provider.Kayenta.Lookback)
		if err != nil {
			return metricutil.MarkMeasurementError(newMeasurement, err)
		}
		scopes[ks.Name] = s
	}

	req := AnalysisRequest{
		Scopes: scopes,
		Thresholds: ThresholdsRequest{
			Pass:     metric.Provider.Kayenta.Threshold.Pass,
			Marginal: metric.Provider.Kayenta.Threshold.Marginal,
		},
	}

	jobPayLoad, err := json.Marshal(req)
	if err != nil {
		return metricutil.MarkMeasurementError(newMeasurement, err)
	}

	response, err := p.client.Post(jobURL, "application/json", bytes.NewBuffer(jobPayLoad))
	if err != nil || response.Body == nil || response.StatusCode != 200 {
		if err == nil {
			err = errors.New("Invalid Response")
		}
		return metricutil.MarkMeasurementError(newMeasurement, err)
	}

	data, err := io.ReadAll(response.Body)
	if err != nil {
		return metricutil.MarkMeasurementError(newMeasurement, err)
	}
	var dat map[string]any
	if err := json.Unmarshal(data, &dat); err != nil {
		return metricutil.MarkMeasurementError(newMeasurement, err)
	}
	jobId := dat["canaryExecutionId"]
	m := make(map[string]string)
	m["canaryExecutionId"] = fmt.Sprintf("%v", jobId)
	if len(m["canaryExecutionId"]) == 0 {

		return metricutil.MarkMeasurementError(newMeasurement, errors.New("Invalid canaryExecutionId"))
	}
	newMeasurement.Metadata = m

	newMeasurement.Phase = v1alpha1.AnalysisPhaseRunning

	resumeTime := metav1.NewTime(timeutil.Now().Add(resumeDelay))
	newMeasurement.ResumeAt = &resumeTime

	return newMeasurement
}

// Resume should not be used the kayenta provider since all the work should occur in the Run method
func (p *Provider) Resume(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, measurement v1alpha1.Measurement) v1alpha1.Measurement {

	scoreURL := fmt.Sprintf(scoreURLFormat, metric.Provider.Kayenta.Address, measurement.Metadata["canaryExecutionId"])

	response, err := p.client.Get(scoreURL)
	if err != nil || response.Body == nil || response.StatusCode != 200 {
		if err == nil {
			err = errors.New("Invalid Response")
		}
		return metricutil.MarkMeasurementError(measurement, err)
	}

	data, err := io.ReadAll(response.Body)
	if err != nil {
		return metricutil.MarkMeasurementError(measurement, err)
	}

	patch := make(map[string]any)

	err = json.Unmarshal(data, &patch)
	if err != nil {
		return metricutil.MarkMeasurementError(measurement, err)
	}

	status, ok, err := unstructured.NestedBool(patch, "complete")
	if ok {
		if !status { // resume later since it is incomplete
			resumeTime := metav1.NewTime(timeutil.Now().Add(resumeDelay))
			measurement.ResumeAt = &resumeTime
			measurement.Phase = v1alpha1.AnalysisPhaseRunning

			return measurement
		}
	} else {
		if err == nil {
			err = errors.New("Missing Complete Status")
		}
		return metricutil.MarkMeasurementError(measurement, err)
	}

	result, ok, err := unstructured.NestedFloat64(patch, "result", "judgeResult", "score", "score")

	if ok {
		score := int(result)
		measurement.Value = fmt.Sprintf("%v", score)
		measurement.Phase = evaluateResult(score, int(metric.Provider.Kayenta.Threshold.Pass), int(metric.Provider.Kayenta.Threshold.Marginal))
	} else {
		if err == nil {
			err = errors.New("Missing Score")
		}
		return metricutil.MarkMeasurementError(measurement, err)
	}

	finishTime := timeutil.MetaNow()
	measurement.FinishedAt = &finishTime

	return measurement
}

func evaluateResult(score int, pass int, marginal int) v1alpha1.AnalysisPhase {
	if score >= pass {
		return v1alpha1.AnalysisPhaseSuccessful
	} else if score < pass && score >= marginal {
		return v1alpha1.AnalysisPhaseInconclusive
	} else {
		return v1alpha1.AnalysisPhaseFailed
	}
}

func getScopeRequest(scope v1alpha1.KayentaScope, experimentStartedAt *metav1.Time, startTime metav1.Time, interval v1alpha1.DurationString, lookback bool) (ScopeRequest, error) {
	if (scope.ControlScope.Start == "") != (scope.ExperimentScope.Start == "") {
		return ScopeRequest{}, errors.New("controlScope.start and experimentScope.start must both be set or be empty")
	}

	if (scope.ControlScope.End == "") != (scope.ExperimentScope.End == "") {
		return ScopeRequest{}, errors.New("controlScope.end and experimentScope.end must both be set or be empty")
	}

	if scope.ControlScope.Start == "" && scope.ExperimentScope.Start == "" {
		start, err := getStartTime(experimentStartedAt, startTime, interval, lookback)
		if err != nil {
			return ScopeRequest{}, err
		}
		scope.ControlScope.Start = start
		scope.ExperimentScope.Start = start
	}

	if scope.ControlScope.End == "" && scope.ExperimentScope.End == "" {
		end := getEndTime(startTime)
		scope.ControlScope.End = end
		scope.ExperimentScope.End = end
	}

	return scopeToScopeRequest(scope)
}

func getStartTime(experimentStartedAt *metav1.Time, currentTime metav1.Time, interval v1alpha1.DurationString, lookback bool) (string, error) {
	var start string
	if lookback {
		start = experimentStartedAt.Format(time.RFC3339)
	} else {
		duration, err := interval.Duration()
		if err != nil {
			return "", fmt.Errorf("Invalid duration %q: %w", interval, err)
		}
		start = (currentTime.Add(-duration)).Format(time.RFC3339)
	}
	return start, nil
}

func getEndTime(currentTime metav1.Time) string {
	return currentTime.Format(time.RFC3339)
}

func scopeToScopeRequest(scope v1alpha1.KayentaScope) (ScopeRequest, error) {
	return ScopeRequest{
		ControlScope:    scope.ControlScope,
		ExperimentScope: scope.ExperimentScope,
	}, nil
}

// Terminate should not be used the kayenta provider since all the work should occur in the Run method
func (p *Provider) Terminate(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, measurement v1alpha1.Measurement) v1alpha1.Measurement {
	p.logCtx.Warn("kayenta provider should not execute the Terminate method")
	return measurement
}

// GarbageCollect is a no-op for the kayenta provider
func (p *Provider) GarbageCollect(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, limit int) error {
	return nil
}

func NewKayentaProvider(logCtx log.Entry, client http.Client) *Provider {
	return &Provider{
		logCtx: logCtx,
		client: client,
	}
}

func NewHttpClient() http.Client {
	c := http.Client{
		Timeout: httpConnectionTimeout,
	}

	return c
}
