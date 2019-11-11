package kayenta

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	log "github.com/sirupsen/logrus"
	"io/ioutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/gjson"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"

	metricutil "github.com/argoproj/argo-rollouts/utils/metric"
	templateutil "github.com/argoproj/argo-rollouts/utils/template"
)

const (
	//ProviderType indicates the provider is kayenta
	ProviderType = "Kayenta"
	KayentaScoreURL string = "{{inputs.address}}/canary/{{inputs.canaryExecutionId}}"

	JobURL string = "{{inputs.address}}/canary/{{inputs.canaryConfigId}}?application={{inputs.application}}&metricsAccountName={{inputs.metricsAccountName}}&configurationAccountName={{inputs.configurationAccountName}}&storageAccountName={{inputs.storageAccountName}}"

	JobPayloadTemplate string = `
							{
								"scopes": {
										{{inputs.scopes}}
								},
                                "thresholds" : {
                                    "pass": {{inputs.pass}},
                                    "marginal": {{inputs.marginal}}
                                }
                            }`


)

type Provider struct {
	logCtx log.Entry
	client http.Client
}

// Type incidates provider is a kayenta provider
func (p *Provider) Type() string {
	return ProviderType
}

// Run queries kayentd for the metric
func (p *Provider) Run(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric) v1alpha1.Measurement {
	startTime := metav1.Now()
	newMeasurement := v1alpha1.Measurement{
		StartedAt: &startTime,
	}

	jobURL := strings.Replace(JobURL, "{{inputs.address}}", metric.Provider.Kayenta.Address, 1)
	jobURL = strings.Replace(jobURL, "{{inputs.canaryConfigId}}", metric.Provider.Kayenta.CanaryConfigId, 1)
	jobURL = strings.Replace(jobURL, "{{inputs.application}}", metric.Provider.Kayenta.Application, 1)
	jobURL = strings.Replace(jobURL, "{{inputs.metricsAccountName}}", metric.Provider.Kayenta.MetricsAccountName, 1)
	jobURL = strings.Replace(jobURL, "{{inputs.configurationAccountName}}", metric.Provider.Kayenta.ConfigurationAccountName, 1)
	jobURL = strings.Replace(jobURL, "{{inputs.storageAccountName}}", metric.Provider.Kayenta.StorageAccountName, 1)

	jobPayLoad := strings.Replace(JobPayloadTemplate, "{{inputs.pass}}", strconv.Itoa(metric.Provider.Kayenta.Threshold.Pass), 1)
	jobPayLoad = strings.Replace(jobPayLoad, "{{inputs.marginal}}", strconv.Itoa(metric.Provider.Kayenta.Threshold.Marginal), 1)
	var scopes string
	for i, s := range metric.Provider.Kayenta.Scopes {
		name := s.Name
		controlScope, err := json.Marshal(s.ControlScope)
		if err != nil {
			return metricutil.MarkMeasurementError(newMeasurement, err)
		}
		controlScopeStr := "\"controlScope\":" + string(controlScope)
		experimentScope, err := json.Marshal(s.ExperimentScope)
		if err != nil {
			return metricutil.MarkMeasurementError(newMeasurement, err)
		}
		experimentScopeStr := "\"experimentScope\":" + string(experimentScope)
		scopes = scopes + "\"" + name +  "\":{" + controlScopeStr + "," + experimentScopeStr + "}"
		if i < (len(metric.Provider.Kayenta.Scopes) - 1) {
			scopes = scopes + ","
		}

	}

	jobPayLoad = strings.Replace(jobPayLoad, "{{inputs.scopes}}", scopes, 1)

	jsonValue, err := templateutil.ResolveArgs(jobPayLoad, run.Spec.Arguments)
	if err != nil {
		return metricutil.MarkMeasurementError(newMeasurement, err)
	}

	response, err := p.client.Post(jobURL, "application/json", bytes.NewBuffer([]byte(jsonValue)))
	if err != nil || response.Body == nil || response.StatusCode > 300 {
		if err == nil {
			err := errors.New("Invalid Response")
			return metricutil.MarkMeasurementError(newMeasurement, err)
		}
		return metricutil.MarkMeasurementError(newMeasurement, err)
	} else {
		data, err := ioutil.ReadAll(response.Body)
		if err != nil {
			return metricutil.MarkMeasurementError(newMeasurement, err)
		}
		var dat map[string]interface{}
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
	}

	newMeasurement.Phase = v1alpha1.AnalysisPhaseRunning

	resumeTime := metav1.NewTime(time.Now().Add(15 * time.Second))
	newMeasurement.ResumeAt = &resumeTime
	finishTime := metav1.Now()
	newMeasurement.FinishedAt = &finishTime

	return newMeasurement
}

// Resume should not be used the kayenta provider since all the work should occur in the Run method
func (p *Provider) Resume(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, measurement v1alpha1.Measurement) v1alpha1.Measurement {

	scoreURL := strings.Replace(KayentaScoreURL, "{{inputs.address}}", metric.Provider.Kayenta.Address, 1)
	scoreURL = strings.Replace(scoreURL, "{{inputs.canaryExecutionId}}", measurement.Metadata["canaryExecutionId"], 1)

	response, err := p.client.Get(scoreURL)
	if err != nil || response.Body == nil || response.StatusCode > 300 {
		if err == nil {
			err := errors.New("Invalid Response")
			return metricutil.MarkMeasurementError(measurement, err)
		}
		return metricutil.MarkMeasurementError(measurement, err)
	} else {
		data, err := ioutil.ReadAll(response.Body)
		if  err != nil {
			return metricutil.MarkMeasurementError(measurement, err)
		}
		json := string(data)
		result := gjson.Get(json, "result.judgeResult.score.score")

		if  len(result.Raw) == 0 || ! isNumeric(result.Raw) {
			return metricutil.MarkMeasurementError(measurement, errors.New("Invalid score"))
		}
		score := int(result.Num)
		measurement.Value = fmt.Sprintf("%v", score)
		measurement.Phase = evaluateResult(score, metric.Provider.Kayenta.Threshold.Pass, metric.Provider.Kayenta.Threshold.Marginal)
	}

	finishTime := metav1.Now()
	measurement.FinishedAt = &finishTime

	return measurement
}

func isNumeric(s string) bool {
	_, err := strconv.ParseFloat(s, 64)
	return err == nil
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
	//TODO:  Should timeout be configurable?
	c := http.Client{
		Timeout: 15 * time.Second,
	}

	return c
}