package opsmx

import (
	"net/http"
	"testing"
	"time"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

const (
	httpConnectionTestTimeout time.Duration = 15 * time.Second
)

func TestRunSuccessfully(t *testing.T) {
	// Test Cases
	var tests = []struct {
		metric        v1alpha1.Metric
		expectedPhase v1alpha1.AnalysisPhase
		expectedValue string
	}{
		{
			metric: v1alpha1.Metric{
				Name: "testappy",
				Provider: v1alpha1.MetricProvider{
					OPSMX: &v1alpha1.OPSMXMetric{
						Gate_url:            "https://ds312.isd-dev.opsmx.net/",
						Application:         "testapp",
						Baseline_start_time: "2022-07-29T13:15:00Z",
						Canary_start_time:   "2022-07-29T13:15:00Z",
						LifetimeHours:       "0.5",
						Threshold: v1alpha1.OPSMXThreshold{
							Pass:     80,
							Marginal: 65,
						},
					},
				},
			},
			expectedValue: "100",
			expectedPhase: v1alpha1.AnalysisPhaseSuccessful,
		},
		{
			metric: v1alpha1.Metric{
				Name: "testapp",
				Provider: v1alpha1.MetricProvider{
					OPSMX: &v1alpha1.OPSMXMetric{
						Gate_url:            "https://ds312.isd-dev.opsmx.net/",
						Application:         "testapp",
						Baseline_start_time: "2022-08-10T13:15:00Z",
						Canary_start_time:   "2022-08-10T13:15:00Z",
						End_time:            "2022-08-10T13:45:10Z",
						Threshold: v1alpha1.OPSMXThreshold{
							Pass:     80,
							Marginal: 65,
						},
					},
				},
			},
			expectedValue: "100",
			expectedPhase: v1alpha1.AnalysisPhaseSuccessful,
		},
		{
			metric: v1alpha1.Metric{
				Name: "testapp",
				Provider: v1alpha1.MetricProvider{
					OPSMX: &v1alpha1.OPSMXMetric{
						Gate_url:            "https://ds312.isd-dev.opsmx.net/",
						Application:         "testapp",
						Baseline_start_time: "",
						Canary_start_time:   "2022-08-10T13:15:00Z",
						End_time:            "2022-08-10T13:45:10Z",
						Threshold: v1alpha1.OPSMXThreshold{
							Pass:     80,
							Marginal: 65,
						},
					},
				},
			},
			expectedValue: "100",
			expectedPhase: v1alpha1.AnalysisPhaseSuccessful,
		},
		{
			metric: v1alpha1.Metric{
				Name: "testapp",
				Provider: v1alpha1.MetricProvider{
					OPSMX: &v1alpha1.OPSMXMetric{
						Gate_url:            "https://ds312.isd-dev.opsmx.net/",
						Application:         "multiservice",
						Baseline_start_time: "",
						Canary_start_time:   "2022-08-10T13:15:00Z",
						End_time:            "2022-08-10T13:45:10Z",
						Threshold: v1alpha1.OPSMXThreshold{
							Pass:     80,
							Marginal: 65,
						},
						Services: []v1alpha1.OPSMXService{
							{
								ServiceName:          "service1",
								GateName:             "gate1",
								MetricScopeVariables: "job_name",
								BaselineMetricScope:  "oes-datascience-br",
								CanaryMetricScope:    "oes-datascience-cr",
							},
						},
					},
				},
			},
			expectedValue: "100",
			expectedPhase: v1alpha1.AnalysisPhaseSuccessful,
		},
		{
			metric: v1alpha1.Metric{
				Name: "testapp",
				Provider: v1alpha1.MetricProvider{
					OPSMX: &v1alpha1.OPSMXMetric{
						Gate_url:            "https://ds312.isd-dev.opsmx.net/",
						Application:         "multiservice",
						Baseline_start_time: "",
						Canary_start_time:   "2022-08-10T13:15:00Z",
						End_time:            "2022-08-10T13:45:10Z",
						Threshold: v1alpha1.OPSMXThreshold{
							Pass:     80,
							Marginal: 65,
						},
						Services: []v1alpha1.OPSMXService{
							{
								ServiceName:          "service1",
								GateName:             "gate1",
								MetricScopeVariables: "job_name",
								BaselineMetricScope:  "oes-sapor-br",
								CanaryMetricScope:    "oes-sapor-cr",
							},
							{
								ServiceName:          "service2",
								GateName:             "gate2",
								MetricScopeVariables: "job_name",
								BaselineMetricScope:  "oes-platform-br",
								CanaryMetricScope:    "oes-platform-cr",
							},
						},
					},
				},
			},
			expectedValue: "100",
			expectedPhase: v1alpha1.AnalysisPhaseSuccessful,
		},
		{
			metric: v1alpha1.Metric{
				Name: "testapp",
				Provider: v1alpha1.MetricProvider{
					OPSMX: &v1alpha1.OPSMXMetric{
						Gate_url:            "https://ds312.isd-dev.opsmx.net/",
						Application:         "multiservice",
						Baseline_start_time: "",
						Canary_start_time:   "2022-08-10T13:15:00Z",
						End_time:            "2022-08-10T13:45:10Z",
						Threshold: v1alpha1.OPSMXThreshold{
							Pass:     80,
							Marginal: 65,
						},
						Services: []v1alpha1.OPSMXService{
							{
								ServiceName:          "service1",
								GateName:             "gate1",
								MetricScopeVariables: "job_name",
								BaselineMetricScope:  "oes-platform-br",
								CanaryMetricScope:    "oes-platform-cr",
							},
							{
								ServiceName:          "service2",
								GateName:             "gate2",
								MetricScopeVariables: "job_name",
								BaselineMetricScope:  "oes-sapor-br",
								CanaryMetricScope:    "oes-sapor-cr",
								LogScopeVariables:    "kubernetes.container_name",
								BaselineLogScope:     "oes-datascience-br",
								CanaryLogScope:       "oes-datascience-cr",
							},
						},
					},
				},
			},
			expectedValue: "100",
			expectedPhase: v1alpha1.AnalysisPhaseSuccessful,
		},
	}
	for _, test := range tests {
		e := log.NewEntry(log.New())
		c := NewTestHttpClient()
		provider := NewOPSMXProvider(*e, c)
		measurement := provider.Run(newAnalysisRun(), test.metric)
		// Phase specific cases
		switch test.expectedPhase {
		case v1alpha1.AnalysisPhaseSuccessful:
			assert.NotNil(t, measurement.StartedAt)
			assert.Equal(t, test.expectedValue, measurement.Value)
			assert.Equal(t, test.expectedPhase, measurement.Phase)
			assert.NotNil(t, measurement.FinishedAt)
		case v1alpha1.AnalysisPhaseFailed:
			assert.NotNil(t, measurement.StartedAt)
			assert.Equal(t, test.expectedPhase, measurement.Phase)
			assert.NotNil(t, measurement.FinishedAt)
		}
	}
	var testing = []struct {
		metric        v1alpha1.Metric
		expectedPhase v1alpha1.AnalysisPhase
	}{
		{
			metric: v1alpha1.Metric{
				Name: "testapp",
				Provider: v1alpha1.MetricProvider{
					OPSMX: &v1alpha1.OPSMXMetric{
						Gate_url:      "https://ds312.isd-dev.opsmx.net/",
						Application:   "testapp",
						LifetimeHours: "0.05",
						Threshold: v1alpha1.OPSMXThreshold{
							Pass:     80,
							Marginal: 65,
						},
					},
				},
			},
		},
		{
			metric: v1alpha1.Metric{
				Name: "testapp",
				Provider: v1alpha1.MetricProvider{
					OPSMX: &v1alpha1.OPSMXMetric{
						Gate_url:            "https://ds312.isd-dev.opsmx.net/",
						Application:         "multiservice",
						Baseline_start_time: "",
						Canary_start_time:   "2022-08-10T13:15:00Z",
						End_time:            "2022-08-10T13:45:10Z",
						Threshold: v1alpha1.OPSMXThreshold{
							Pass:     80,
							Marginal: 65,
						},
						Services: []v1alpha1.OPSMXService{
							{
								ServiceName:          "service1",
								GateName:             "gate1",
								LogScopeVariables:    "kubernetes.container_name",
								BaselineLogScope:     "oes-datascience-br",
								CanaryLogScope:       "oes-datascience-cr",
								MetricScopeVariables: "job_name",
								BaselineMetricScope:  "oes-sapor-br",
								CanaryMetricScope:    "oes-sapor-cr",
							},
						},
					},
				},
			},
			expectedPhase: v1alpha1.AnalysisPhaseError,
		},
		{
			metric: v1alpha1.Metric{
				Name: "testapp",
				Provider: v1alpha1.MetricProvider{
					OPSMX: &v1alpha1.OPSMXMetric{
						Gate_url:    "https://ds312.isd-dev.opsmx.net/",
						Application: "testapp",
						Threshold: v1alpha1.OPSMXThreshold{
							Pass:     80,
							Marginal: 65,
						},
					},
				},
			},
			expectedPhase: v1alpha1.AnalysisPhaseError,
		},
		{
			metric: v1alpha1.Metric{
				Name: "testapp",
				Provider: v1alpha1.MetricProvider{
					OPSMX: &v1alpha1.OPSMXMetric{
						Gate_url:            "https://ds312.isd-dev.opsmx.net/",
						Application:         "testapp",
						Baseline_start_time: "2022-08-02T13:15:00Z",
						Canary_start_time:   "2022-08-02T13:15:00Z",
						LifetimeHours:       "0.05",
						Threshold: v1alpha1.OPSMXThreshold{
							Pass:     60,
							Marginal: 80,
						},
					},
				},
			},
			expectedPhase: v1alpha1.AnalysisPhaseError,
		},
		{
			metric: v1alpha1.Metric{
				Name: "testapp",
				Provider: v1alpha1.MetricProvider{
					OPSMX: &v1alpha1.OPSMXMetric{
						Gate_url:            "https://ds312.isd-dev.opsmx.net/",
						Application:         "testapp",
						Baseline_start_time: "2022-08-02T13:15:00Z",
						Canary_start_time:   "2022-O8-02T13:15:00Z",
						LifetimeHours:       "0.05",
						Threshold: v1alpha1.OPSMXThreshold{
							Pass:     80,
							Marginal: 60,
						},
					},
				},
			},
			expectedPhase: v1alpha1.AnalysisPhaseError,
		},
		{
			metric: v1alpha1.Metric{
				Name: "testapp",
				Provider: v1alpha1.MetricProvider{
					OPSMX: &v1alpha1.OPSMXMetric{
						Gate_url:            "https://ds312.isd-dev.opsmx.net/",
						Application:         "testap",
						Baseline_start_time: "2022-08-02T13:15:00Z",
						Canary_start_time:   "2022-08-02T13:15:00Z",
						LifetimeHours:       "0.05",
						Threshold: v1alpha1.OPSMXThreshold{
							Pass:     80,
							Marginal: 60,
						},
					},
				},
			},
			expectedPhase: v1alpha1.AnalysisPhaseError,
		},
	}
	for _, test := range testing {
		e := log.NewEntry(log.New())
		c := NewTestHttpClient()
		provider := NewOPSMXProvider(*e, c)
		measurement := provider.Run(newAnalysisRun(), test.metric)
		// Phase specific cases
		switch test.expectedPhase {
		case v1alpha1.AnalysisPhaseSuccessful:
			assert.NotNil(t, measurement.StartedAt)
			assert.NotNil(t, measurement.FinishedAt)
		case v1alpha1.AnalysisPhaseFailed:
			assert.NotNil(t, measurement.StartedAt)
			assert.Equal(t, test.expectedPhase, measurement.Phase)
			assert.NotNil(t, measurement.FinishedAt)
		case v1alpha1.AnalysisPhaseError:
			assert.NotNil(t, measurement.StartedAt)
			assert.Equal(t, test.expectedPhase, measurement.Phase)
			assert.NotNil(t, measurement.FinishedAt)
		}
	}

}

func newAnalysisRun() *v1alpha1.AnalysisRun {
	return &v1alpha1.AnalysisRun{}
}

func NewTestHttpClient() http.Client {
	c := http.Client{
		Timeout: httpConnectionTestTimeout,
	}
	return c
}
