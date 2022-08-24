package opsmx

import (
	"encoding/json"

	"math"

	"net/http"

	"fmt"
	"io/ioutil"
	"regexp"
	"strconv"
	"strings"

	"time"

	"errors"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	metricutil "github.com/argoproj/argo-rollouts/utils/metric"
	timeutil "github.com/argoproj/argo-rollouts/utils/time"
	log "github.com/sirupsen/logrus"
)

const (
	ProviderType                          = "opsmx"
	configIdLookupURLFormat               = `%s/autopilot/api/v3/registerCanary`
	scoreUrlFormat                        = `%s/autopilot/canaries/%s`
	reportUrlFormat                       = `%sui/application/deploymentverification/%s/%s`
	httpConnectionTimeout   time.Duration = 15 * time.Second
	DefaultjobPayloadFormat               = `{
        "application": "%s",
        "canaryConfig": {
                "lifetimeHours": %s,
                "canaryHealthCheckHandler": {
                                "minimumCanaryResultScore": %s
                                },
                "canarySuccessCriteria": {
                            "canaryResultScore": %s
                                }
                },
        "canaryDeployments": [
                    {
                    "canaryStartTimeMs": %s,
                    "baselineStartTimeMs": %s
                    }
          ]
    }`
	JobPayloadwServices = `{
        "application": "%s",
        "canaryConfig": {
                "lifetimeHours": %s,
                "canaryHealthCheckHandler": {
                                "minimumCanaryResultScore": %s
                                },
                "canarySuccessCriteria": {
                            "canaryResultScore": %s
                                }
                },
        "canaryDeployments": [
                    {
                    "canaryStartTimeMs": %s,
                    "baselineStartTimeMs": %s,
					%s
                    }
          ]
    }`

	ServicesjobPayloadFormat = `"canary":{
			%s
		},
		"baseline":{
			%s
		}`
	logPayloadFormat = `"log": {
			%s
		}`
	metricPayloadFormat = `"metric": {
			%s
		}`
	internalFormat = `"%s": {
		"serviceGate": "%s",
		"%s": "%s"
		}`
)

type Provider struct {
	logCtx log.Entry
	client http.Client
}

// Type indicates provider is a OPSMX provider
func (p *Provider) Type() string {
	return ProviderType
}

// GetMetadata returns any additional metadata which needs to be stored & displayed as part of the metrics result.
func (p *Provider) GetMetadata(metric v1alpha1.Metric) map[string]string {
	return nil
}

func isNil(i interface{}) bool {
	return i == nil
}

func roundFloat(val float64, precision uint) float64 {
	ratio := math.Pow(10, float64(precision))
	return math.Round(val*ratio) / ratio
}

// Run queries opsmx for the metric
func (p *Provider) Run(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric) v1alpha1.Measurement {

	var (
		canaryStartTime   string
		baselineStartTime string

		configIdLookupURL string
		jobPayload        string
		scoreURL          string
		reportUrl         string

		baselinelog    string
		canarylog      string
		baselinemetric string
		canarymetric   string

		ServiceJobPayload     string
		baselinemetricPayload string
		canarymetricPayload   string
		baselinelogPayload    string
		canarylogPayload      string
		baselinePayload       string
		canaryPayload         string

		canaryId    string
		canaryScore string

		canary     map[string]interface{}
		status     map[string]interface{}
		result     map[string]interface{}
		finalScore map[string]interface{}
	)

	startTime := timeutil.MetaNow()
	newMeasurement := v1alpha1.Measurement{
		StartedAt: &startTime,
	}

	configIdLookupURL = fmt.Sprintf(configIdLookupURLFormat, metric.Provider.OPSMX.Gate_url)

	if metric.Provider.OPSMX.Canary_start_time == "" && metric.Provider.OPSMX.Baseline_start_time == "" && metric.Provider.OPSMX.LifetimeHours == "" {
		err := errors.New("either provide lifetimehours or start time")
		return metricutil.MarkMeasurementError(newMeasurement, err)
	}

	if (metric.Provider.OPSMX.Canary_start_time == "" && metric.Provider.OPSMX.Baseline_start_time != "") || (metric.Provider.OPSMX.Canary_start_time != "" && metric.Provider.OPSMX.Baseline_start_time == "") {
		if metric.Provider.OPSMX.Canary_start_time == "" {
			metric.Provider.OPSMX.Canary_start_time = metric.Provider.OPSMX.Baseline_start_time
		} else {
			metric.Provider.OPSMX.Baseline_start_time = metric.Provider.OPSMX.Canary_start_time
		}
	}

	if metric.Provider.OPSMX.Canary_start_time == "" && metric.Provider.OPSMX.Baseline_start_time == "" {
		tm := time.Now()
		canaryStartTime = fmt.Sprintf("%d", tm.UnixNano()/int64(time.Millisecond))
		baselineStartTime = fmt.Sprintf("%d", tm.UnixNano()/int64(time.Millisecond))
	} else {
		ts_start, err := time.Parse(time.RFC3339, metric.Provider.OPSMX.Canary_start_time) //make a time object for canary start time
		if err != nil {
			return metricutil.MarkMeasurementError(newMeasurement, err)
		}
		canaryStartTime = fmt.Sprintf("%d", ts_start.UnixNano()/int64(time.Millisecond)) //convert ISO to epoch

		ts_start, err = time.Parse(time.RFC3339, metric.Provider.OPSMX.Baseline_start_time) //make a time object for baseline start time
		if err != nil {
			return metricutil.MarkMeasurementError(newMeasurement, err)
		}
		baselineStartTime = fmt.Sprintf("%d", ts_start.UnixNano()/int64(time.Millisecond)) //convert ISO to epoch
	}

	if metric.Provider.OPSMX.LifetimeHours == "" && metric.Provider.OPSMX.End_time == "" {
		err := errors.New("either provide lifetimehours or end time")
		return metricutil.MarkMeasurementError(newMeasurement, err)
	}

	//If lifetimeHours not given
	if metric.Provider.OPSMX.LifetimeHours == "" {
		ts_start, _ := time.Parse(time.RFC3339, metric.Provider.OPSMX.Canary_start_time)
		ts_end, _ := time.Parse(time.RFC3339, metric.Provider.OPSMX.End_time)
		ts_difference := ts_end.Sub(ts_start)
		min, _ := time.ParseDuration(ts_difference.String())
		metric.Provider.OPSMX.LifetimeHours = fmt.Sprintf("%v", roundFloat(min.Minutes()/60, 1))
	}

	if metric.Provider.OPSMX.Threshold.Pass <= metric.Provider.OPSMX.Threshold.Marginal {
		err := errors.New("pass score cannot be greater than marginal score")
		return metricutil.MarkMeasurementError(newMeasurement, err)
	}

	if metric.Provider.OPSMX.Services == nil {
		jobPayload = fmt.Sprintf(DefaultjobPayloadFormat, metric.Provider.OPSMX.Application, metric.Provider.OPSMX.LifetimeHours, fmt.Sprintf("%d", metric.Provider.OPSMX.Threshold.Marginal), fmt.Sprintf("%d", metric.Provider.OPSMX.Threshold.Pass), canaryStartTime, baselineStartTime) //Make the payload
	} else {
		for _, item := range metric.Provider.OPSMX.Services {
			if item.LogScopeVariables != "" {
				if len(strings.Split(item.LogScopeVariables, ",")) != len(strings.Split(item.BaselineLogScope, ",")) || len(strings.Split(item.LogScopeVariables, ",")) != len(strings.Split(item.CanaryLogScope, ",")) {
					err := errors.New("mismatch in amount of log scope variables and baseline/canary log scope")
					return metricutil.MarkMeasurementError(newMeasurement, err)
				}
				if baselinelog == "" {
					baselinelog = fmt.Sprintf(internalFormat, item.ServiceName, item.GateName, item.LogScopeVariables, item.BaselineLogScope)
					canarylog = fmt.Sprintf(internalFormat, item.ServiceName, item.GateName, item.LogScopeVariables, item.CanaryLogScope)
				} else {
					temp := fmt.Sprintf(internalFormat, item.ServiceName, item.GateName, item.LogScopeVariables, item.BaselineLogScope)
					baselinelog = fmt.Sprintf("%s\n,\n%s", baselinelog, temp)
					temp = fmt.Sprintf(internalFormat, item.ServiceName, item.GateName, item.LogScopeVariables, item.CanaryLogScope)
					canarylog = fmt.Sprintf("%s\n,\n%s", canarylog, temp)
				}
			}
			if item.MetricScopeVariables != "" {
				if len(strings.Split(item.MetricScopeVariables, ",")) != len(strings.Split(item.BaselineMetricScope, ",")) || len(strings.Split(item.MetricScopeVariables, ",")) != len(strings.Split(item.CanaryMetricScope, ",")) {
					err := errors.New("mismatch in amount of metric scope variables and baseline/canary metric scope")
					return metricutil.MarkMeasurementError(newMeasurement, err)
				}
				if baselinemetric == "" {
					baselinemetric = fmt.Sprintf(internalFormat, item.ServiceName, item.GateName, item.MetricScopeVariables, item.BaselineMetricScope)
					canarymetric = fmt.Sprintf(internalFormat, item.ServiceName, item.GateName, item.MetricScopeVariables, item.CanaryMetricScope)
				} else {
					temp := fmt.Sprintf(internalFormat, item.ServiceName, item.GateName, item.MetricScopeVariables, item.BaselineMetricScope)
					baselinemetric = fmt.Sprintf("%s\n,\n%s", baselinemetric, temp)
					temp = fmt.Sprintf(internalFormat, item.ServiceName, item.GateName, item.MetricScopeVariables, item.CanaryMetricScope)
					canarymetric = fmt.Sprintf("%s\n,\n%s", canarymetric, temp)
				}
			}
		}

		if baselinelog != "" {
			baselinelogPayload = fmt.Sprintf(logPayloadFormat, baselinelog)
			canarylogPayload = fmt.Sprintf(logPayloadFormat, canarylog)
		}

		if baselinemetric != "" {
			baselinemetricPayload = fmt.Sprintf(metricPayloadFormat, baselinemetric)
			canarymetricPayload = fmt.Sprintf(metricPayloadFormat, canarymetric)
		}

		if baselinelogPayload == "" && baselinemetricPayload == "" {
			err := errors.New("either provide log or metric arguments")
			return metricutil.MarkMeasurementError(newMeasurement, err)
		}

		if baselinelogPayload != "" && baselinemetricPayload != "" {
			baselinePayload = fmt.Sprintf("%s,\n%s", baselinelogPayload, baselinemetricPayload)
			canaryPayload = fmt.Sprintf("%s,\n%s", canarylogPayload, canarymetricPayload)
			ServiceJobPayload = fmt.Sprintf(ServicesjobPayloadFormat, canaryPayload, baselinePayload)
		} else {
			if baselinelogPayload != "" && baselinemetricPayload == "" {
				ServiceJobPayload = fmt.Sprintf(ServicesjobPayloadFormat, canarylogPayload, baselinelogPayload)
			} else {
				ServiceJobPayload = fmt.Sprintf(ServicesjobPayloadFormat, canarymetricPayload, baselinemetricPayload)
			}
		}
		jobPayload = fmt.Sprintf(JobPayloadwServices, metric.Provider.OPSMX.Application, metric.Provider.OPSMX.LifetimeHours, fmt.Sprintf("%d", metric.Provider.OPSMX.Threshold.Marginal), fmt.Sprintf("%d", metric.Provider.OPSMX.Threshold.Pass), canaryStartTime, baselineStartTime, ServiceJobPayload)
	}

	// create a request object
	reqBody := strings.NewReader(jobPayload)
	req, err := http.NewRequest(
		"POST",
		configIdLookupURL,
		reqBody,
	)

	if err != nil {
		return metricutil.MarkMeasurementError(newMeasurement, err)
	}
	// add a request header
	req.Header.Add("x-spinnaker-user", "admin")
	req.Header.Add("Content-Type", "application/json")

	// send an HTTP using `req` object
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return metricutil.MarkMeasurementError(newMeasurement, err)
	}

	data, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return metricutil.MarkMeasurementError(newMeasurement, err)
	}
	// close response body
	res.Body.Close()

	checkvalid := json.Valid(data)
	if checkvalid {
		json.Unmarshal(data, &canary)
		if isNil(canary["message"]) {
			canaryId = fmt.Sprintf("%#v", canary["canaryId"])
		} else {
			str1 := fmt.Sprintf("%#v", canary["message"])
			if len(strings.Split(str1, "message")) > 1 {
				str1 = strings.Split(strings.Split(str1, "message")[1], ",")[0]
				re, _ := regexp.Compile(`[^\w]`)
				str1 = re.ReplaceAllString(str1, " ")
				str1 = strings.TrimSpace(str1)
				str1 = strings.ReplaceAll(str1, "   ", " ")
				err = errors.New(str1)
				return metricutil.MarkMeasurementError(newMeasurement, err)
			} else {
				err = errors.New(str1)
				return metricutil.MarkMeasurementError(newMeasurement, err)
			}
		}
	} else {
		err = errors.New("invalid Response")
		return metricutil.MarkMeasurementError(newMeasurement, err)
	}

	scoreURL = fmt.Sprintf(scoreUrlFormat, metric.Provider.OPSMX.Gate_url, canaryId)
	reportUrl = fmt.Sprintf(reportUrlFormat, metric.Provider.OPSMX.Gate_url, metric.Provider.OPSMX.Application, canaryId)

	req, _ = http.NewRequest(
		"GET",
		scoreURL,
		nil,
	)
	req.Header.Set("x-spinnaker-user", metric.Provider.OPSMX.User)
	req.Header.Set("Content-Type", "application/json")

	// Send Request
	res, err = p.client.Do(req)
	if err != nil {
		return metricutil.MarkMeasurementError(newMeasurement, err)
	}

	data, err = ioutil.ReadAll(res.Body)
	if err != nil {
		return metricutil.MarkMeasurementError(newMeasurement, err)
	}
	process := "RUNNING"

	//check till the system has finished running
	for process == "RUNNING" {
		json.Unmarshal(data, &status)
		a, _ := json.MarshalIndent(status["status"], "", "   ")
		json.Unmarshal(a, &status)
		if status["status"] != "RUNNING" {
			process = "COMPLETED"
		} else {

			time.Sleep(3 * time.Second)

			res, err = p.client.Do(req)
			if err != nil {
				return metricutil.MarkMeasurementError(newMeasurement, err)
			}
			data, err = ioutil.ReadAll(res.Body)
			if err != nil {
				return metricutil.MarkMeasurementError(newMeasurement, err)
			}
		}
	}
	res.Body.Close()

	checkvalid = json.Valid(data)
	if checkvalid {
		json.Unmarshal(data, &result)
		jsonBytes, _ := json.MarshalIndent(result["canaryResult"], "", "   ")
		json.Unmarshal(jsonBytes, &finalScore)
		if isNil(finalScore["overallScore"]) {
			canaryScore = "0"
		} else {
			canaryScore = fmt.Sprintf("%v", finalScore["overallScore"])
		}
	} else {
		err = errors.New("invalid Response")
		return metricutil.MarkMeasurementError(newMeasurement, err)
	}

	score, _ := strconv.Atoi(canaryScore)
	newMeasurement.Value = canaryScore
	newMeasurement.Phase = evaluateResult(score, int(metric.Provider.OPSMX.Threshold.Pass), int(metric.Provider.OPSMX.Threshold.Marginal))
	newMeasurement.Message = fmt.Sprintf("Report URL: %s", reportUrl)
	finishTime := timeutil.MetaNow()
	newMeasurement.FinishedAt = &finishTime
	return newMeasurement
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

// Resume should not be used the WebMetric provider since all the work should occur in the Run method
func (p *Provider) Resume(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, measurement v1alpha1.Measurement) v1alpha1.Measurement {
	p.logCtx.Warn("OPSMX provider should not execute the Resume method")
	return measurement
}

// Terminate should not be used the OPSMX provider since all the work should occur in the Run method
func (p *Provider) Terminate(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, measurement v1alpha1.Measurement) v1alpha1.Measurement {
	p.logCtx.Warn("OPSMX provider should not execute the Terminate method")
	return measurement
}

// GarbageCollect is a no-op for the OPSMX provider
func (p *Provider) GarbageCollect(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, limit int) error {
	return nil
}

func NewOPSMXProvider(logCtx log.Entry, client http.Client) *Provider {
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
