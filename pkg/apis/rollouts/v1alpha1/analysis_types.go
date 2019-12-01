package v1alpha1

import (
	"time"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AnalysisTemplate holds the template for performing canary analysis
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type AnalysisTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec AnalysisTemplateSpec `json:"spec"`
}

// AnalysisTemplateList is a list of AnalysisTemplate resources
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type AnalysisTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	// +listType=set
	Items []AnalysisTemplate `json:"items"`
}

// AnalysisTemplateSpec is the specification for a AnalysisTemplate resource
type AnalysisTemplateSpec struct {
	// Metrics contains the list of metrics to query as part of an analysis run
	// +listType=map
	// +listMapKey=name
	Metrics []Metric `json:"metrics"`
	// Args are the list of arguments to the template
	// +optional
	// +listType=map
	// +listMapKey=name
	Args []Argument `json:"args,omitempty"`
}

// DurationString is a string representing a duration (e.g. 30s, 5m, 1h)
type DurationString string

func (d DurationString) Duration() (time.Duration, error) {
	return time.ParseDuration(string(d))
}

// Metric defines a metric in which to perform analysis
type Metric struct {
	// Name is the name of the metric
	Name string `json:"name"`
	// Interval defines an interval string (e.g. 30s, 5m, 1h) between each measurement.
	// If omitted, will perform a single measurement
	Interval DurationString `json:"interval,omitempty"`
	// Count is the number of times to run the measurement. If both interval and count are omitted,
	// the effective count is 1. If only interval is specified, metric runs indefinitely.
	// If count > 1, interval must be specified.
	Count int32 `json:"count,omitempty"`
	// SuccessCondition is an expression which determines if a measurement is considered successful
	// Expression is a goevaluate expression. The keyword `result` is a variable reference to the
	// value of measurement. Results can be both structured data or primitive.
	// Examples:
	//   result > 10
	//   (result.requests_made * result.requests_succeeded / 100) >= 90
	SuccessCondition string `json:"successCondition,omitempty"`
	// FailureCondition is an expression which determines if a measurement is considered failed
	// If both success and failure conditions are specified, and the measurement does not fall into
	// either condition, the measurement is considered Inconclusive
	FailureCondition string `json:"failureCondition,omitempty"`
	// FailureLimit is the maximum number of times the measurement is allowed to fail, before the
	// entire metric is considered Failed (default: 0)
	FailureLimit int32 `json:"failureLimit,omitempty"`
	// InconclusiveLimit is the maximum number of times the measurement is allowed to measure
	// Inconclusive, before the entire metric is considered Inconclusive (default: 0)
	InconclusiveLimit int32 `json:"inconclusiveLimit,omitempty"`
	// ConsecutiveErrorLimit is the maximum number of times the measurement is allowed to error in
	// succession, before the metric is considered error (default: 4)
	ConsecutiveErrorLimit *int32 `json:"consecutiveErrorLimit,omitempty"`
	// Provider configuration to the external system to use to verify the analysis
	Provider MetricProvider `json:"provider"`
}

// EffectiveCount is the effective count based on whether or not count/interval is specified
// If neither count or interval is specified, the effective count is 1
// If only interval is specified, metric runs indefinitely and there is no effective count (nil)
// Otherwise, it is the user specified value
func (m *Metric) EffectiveCount() *int32 {
	if m.Count == 0 {
		if m.Interval == "" {
			one := int32(1)
			return &one
		}
		return nil
	}
	return &m.Count
}

// MetricProvider which external system to use to verify the analysis
// Only one of the fields in this struct should be non-nil
type MetricProvider struct {
	// Prometheus specifies the prometheus metric to query
	Prometheus *PrometheusMetric `json:"prometheus,omitempty"`
	Kayenta    *KayentaMetric    `json:"kayenta,omitempty"`
	// Job specifies the job metric run
	Job *JobMetric `json:"job,omitempty"`
}

// AnalysisPhase is the overall phase of an AnalysisRun, MetricResult, or Measurement
type AnalysisPhase string

// Possible AnalysisPhase values
const (
	AnalysisPhasePending      AnalysisPhase = "Pending"
	AnalysisPhaseRunning      AnalysisPhase = "Running"
	AnalysisPhaseSuccessful   AnalysisPhase = "Successful"
	AnalysisPhaseFailed       AnalysisPhase = "Failed"
	AnalysisPhaseError        AnalysisPhase = "Error"
	AnalysisPhaseInconclusive AnalysisPhase = "Inconclusive"
)

// Completed returns whether or not the analysis status is considered completed
func (as AnalysisPhase) Completed() bool {
	switch as {
	case AnalysisPhaseSuccessful, AnalysisPhaseFailed, AnalysisPhaseError, AnalysisPhaseInconclusive:
		return true
	}
	return false
}

// PrometheusMetric defines the prometheus query to perform canary analysis
type PrometheusMetric struct {
	// Address is the HTTP address and port of the prometheus server
	Address string `json:"address,omitempty"`
	// Query is a raw prometheus query to perform
	Query string `json:"query,omitempty"`
}

// JobMetric defines a job to run which acts as a metric
type JobMetric struct {
	Metadata metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec     batchv1.JobSpec   `json:"spec"`
}

// AnalysisRun is an instantiation of an AnalysisTemplate
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:path=analysisruns
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.phase",description="AnalysisRun status"
type AnalysisRun struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              AnalysisRunSpec   `json:"spec"`
	Status            AnalysisRunStatus `json:"status,omitempty"`
}

// AnalysisRunList is a list of AnalysisTemplate resources
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type AnalysisRunList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	// +listType=atomic
	Items []AnalysisRun `json:"items"`
}

// AnalysisRunSpec is the spec for a AnalysisRun resource
type AnalysisRunSpec struct {
	// Metrics contains the list of metrics to query as part of an analysis run
	// +listType=map
	// +listMapKey=name
	Metrics []Metric `json:"metrics"`
	// Args are the list of arguments used in this run
	// +optional
	// +listType=map
	// +listMapKey=name
	Args []Argument `json:"args,omitempty"`
	// Terminate is used to prematurely stop the run (e.g. rollout completed and analysis is no longer desired)
	Terminate bool `json:"terminate,omitempty"`
}

// Argument is an argument to an AnalysisRun
type Argument struct {
	// Name is the name of the argument
	Name string `json:"name"`
	// Value is the value of the argument
	// +optional
	Value *string `json:"value,omitempty"`
}

// AnalysisRunStatus is the status for a AnalysisRun resource
type AnalysisRunStatus struct {
	// Phase is the status of the analysis run
	Phase AnalysisPhase `json:"phase"`
	// Message is a message explaining current status
	Message string `json:"message,omitempty"`
	// MetricResults contains the metrics collected during the run
	// +listType=map
	// +listMapKey=name
	MetricResults []MetricResult `json:"metricResults,omitempty"`
}

// MetricResult contain a list of the most recent measurements for a single metric along with
// counters on how often the measurement
type MetricResult struct {
	// Name is the name of the metric
	Name string `json:"name"`
	// Phase is the overall aggregate status of the metric
	Phase AnalysisPhase `json:"phase"`
	// Measurements holds the most recent measurements collected for the metric
	// +listType=atomic
	Measurements []Measurement `json:"measurements,omitempty"`
	// Message contains a message describing current condition (e.g. error messages)
	Message string `json:"message,omitempty"`
	// Count is the number of times the metric was measured without Error
	// This is equal to the sum of Successful, Failed, Inconclusive
	Count int32 `json:"count,omitempty"`
	// Successful is the number of times the metric was measured Successful
	Successful int32 `json:"successful,omitempty"`
	// Failed is the number of times the metric was measured Failed
	Failed int32 `json:"failed,omitempty"`
	// Inconclusive is the number of times the metric was measured Inconclusive
	Inconclusive int32 `json:"inconclusive,omitempty"`
	// Error is the number of times an error was encountered during measurement
	Error int32 `json:"error,omitempty"`
	// ConsecutiveError is the number of times an error was encountered during measurement in succession
	// Resets to zero when non-errors are encountered
	ConsecutiveError int32 `json:"consecutiveError,omitempty"`
}

// Measurement is a point in time result value of a single metric, and the time it was measured
type Measurement struct {
	// Phase is the status of this single measurement
	Phase AnalysisPhase `json:"phase"`
	// Message contains a message describing current condition (e.g. error messages)
	Message string `json:"message,omitempty"`
	// StartedAt is the timestamp in which this measurement started to be measured
	StartedAt *metav1.Time `json:"startedAt,omitempty"`
	// FinishedAt is the timestamp in which this measurement completed and value was collected
	FinishedAt *metav1.Time `json:"finishedAt,omitempty"`
	// Value is the measured value of the metric
	Value string `json:"value,omitempty"`
	// Metadata stores additional metadata about this metric result, used by the different providers
	// (e.g. kayenta run ID, job name)
	Metadata map[string]string `json:"metadata,omitempty"`
	// ResumeAt is the  timestamp when the analysisRun should try to resume the measurement
	ResumeAt *metav1.Time `json:"resumeAt,omitempty"`
}

type KayentaMetric struct {
	Address string `json:"address"`

	Application string `json:"application"`

	CanaryConfigName string `json:"canaryConfigName"`

	MetricsAccountName       string `json:"metricsAccountName"`
	ConfigurationAccountName string `json:"configurationAccountName"`
	StorageAccountName       string `json:"storageAccountName"`

	Threshold KayentaThreshold `json:"threshold"`
	// +listType=map
	// +listMapKey=name
	Scopes []KayentaScope `json:"scopes"`
}

type KayentaThreshold struct {
	Pass     int `json:"pass"`
	Marginal int `json:"marginal"`
}

type KayentaScope struct {
	Name            string      `json:"name"`
	ControlScope    ScopeDetail `json:"controlScope"`
	ExperimentScope ScopeDetail `json:"experimentScope"`
}

type ScopeDetail struct {
	Scope  string `json:"scope"`
	Region string `json:"region"`
	Step   int    `json:"step"`
	Start  string `json:"start"`
	End    string `json:"end"`
}
