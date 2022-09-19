package opsmx

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"testing"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	log "github.com/sirupsen/logrus"
	"github.com/tj/assert"
)

var successfulTests = []struct {
	metric                v1alpha1.Metric
	payloadRegisterCanary string
	reportUrl             string
}{
	//Test case for basic function of Single Service feature
	{
		metric: v1alpha1.Metric{
			Name: "testapp",
			Provider: v1alpha1.MetricProvider{
				OPSMX: &v1alpha1.OPSMXMetric{
					GateUrl:           "https://opsmx.test.tst",
					User:              "admin",
					Application:       "multiservice",
					BaselineStartTime: "2022-08-10T13:15:00Z",
					CanaryStartTime:   "2022-08-10T13:15:00Z",
					LifetimeHours:     "0.5",
					Threshold: v1alpha1.OPSMXThreshold{
						Pass:     80,
						Marginal: 65,
					},
					Services: []v1alpha1.OPSMXService{
						{
							MetricScopeVariables:  "job_name",
							BaselineMetricScope:   "oes-datascience-br",
							CanaryMetricScope:     "oes-datascience-cr",
							MetricTemplateName:    "metricTemplate",
							MetricTemplateVersion: "1",
						},
					},
				},
			},
		},
		payloadRegisterCanary: `{
			"application": "multiservice",
			"canaryConfig": {
					"lifetimeHours": "0.5",
					"canaryHealthCheckHandler": {
									"minimumCanaryResultScore": "65"
									},
					"canarySuccessCriteria": {
								"canaryResultScore": "80"
									}
					},
			"canaryDeployments": [
						{
						"canaryStartTimeMs": "1660137300000",
						"baselineStartTimeMs": "1660137300000",
						"canary": {
							"metric": {"service1":{"serviceGate":"gate1","job_name":"oes-datascience-cr","template":"metricTemplate","templateVersion":"1"}
						  }},
						"baseline": {
							"metric": {"service1":{"serviceGate":"gate1","job_name":"oes-datascience-br","template":"metricTemplate","templateVersion":"1"}}
						  }
						}
			  ]
		}`,
		reportUrl: "https://opsmx.test.tst/ui/application/deploymentverification/multiservice/1424",
	},
	//Test case for endtime function of Single Service feature
	{
		metric: v1alpha1.Metric{
			Name: "testapp",
			Provider: v1alpha1.MetricProvider{
				OPSMX: &v1alpha1.OPSMXMetric{
					GateUrl:           "https://opsmx.test.tst",
					User:              "admin",
					Application:       "multiservice",
					BaselineStartTime: "2022-08-10T13:15:00Z",
					CanaryStartTime:   "2022-08-10T13:15:00Z",
					EndTime:           "2022-08-10T13:45:10Z",
					Threshold: v1alpha1.OPSMXThreshold{
						Pass:     80,
						Marginal: 65,
					},
					Services: []v1alpha1.OPSMXService{
						{
							MetricScopeVariables:  "job_name",
							BaselineMetricScope:   "oes-datascience-br",
							CanaryMetricScope:     "oes-datascience-cr",
							MetricTemplateName:    "metricTemplate",
							MetricTemplateVersion: "1",
						},
					},
				},
			},
		},
		payloadRegisterCanary: `{
			"application": "multiservice",
			"canaryConfig": {
					"lifetimeHours": "0.5",
					"canaryHealthCheckHandler": {
									"minimumCanaryResultScore": "65"
									},
					"canarySuccessCriteria": {
								"canaryResultScore": "80"
									}
					},
			"canaryDeployments": [
						{
						"canaryStartTimeMs": "1660137300000",
						"baselineStartTimeMs": "1660137300000",
						"canary": {
							"metric": {"service1":{"serviceGate":"gate1","job_name":"oes-datascience-cr","template":"metricTemplate","templateVersion":"1"}
						  }},
						"baseline": {
							"metric": {"service1":{"serviceGate":"gate1","job_name":"oes-datascience-br","template":"metricTemplate","templateVersion":"1"}}
						  }
						}
			  ]
		}`,
		reportUrl: "https://opsmx.test.tst/ui/application/deploymentverification/multiservice/1424",
	},
	//Test case for only 1 time stamp given function of Single Service feature
	{
		metric: v1alpha1.Metric{
			Name: "testapp",
			Provider: v1alpha1.MetricProvider{
				OPSMX: &v1alpha1.OPSMXMetric{
					GateUrl:           "https://opsmx.test.tst",
					User:              "admin",
					Application:       "multiservice",
					BaselineStartTime: "",
					CanaryStartTime:   "2022-08-10T13:15:00Z",
					EndTime:           "2022-08-10T13:45:10Z",
					Threshold: v1alpha1.OPSMXThreshold{
						Pass:     80,
						Marginal: 65,
					},
					Services: []v1alpha1.OPSMXService{
						{
							MetricScopeVariables:  "job_name",
							BaselineMetricScope:   "oes-datascience-br",
							CanaryMetricScope:     "oes-datascience-cr",
							MetricTemplateName:    "metricTemplate",
							MetricTemplateVersion: "1",
						},
					},
				},
			},
		},
		payloadRegisterCanary: `{
			"application": "multiservice",
			"canaryConfig": {
					"lifetimeHours": "0.5",
					"canaryHealthCheckHandler": {
									"minimumCanaryResultScore": "65"
									},
					"canarySuccessCriteria": {
								"canaryResultScore": "80"
									}
					},
			"canaryDeployments": [
						{
						"canaryStartTimeMs": "1660137300000",
						"baselineStartTimeMs": "1660137300000",
						"canary": {
							"metric": {"service1":{"serviceGate":"gate1","job_name":"oes-datascience-cr","template":"metricTemplate","templateVersion":"1"}
						  }},
						"baseline": {
							"metric": {"service1":{"serviceGate":"gate1","job_name":"oes-datascience-br","template":"metricTemplate","templateVersion":"1"}}
						  }
						}
			  ]
		}`,
		reportUrl: "https://opsmx.test.tst/ui/application/deploymentverification/multiservice/1424",
	},
	//Test case for multi-service feature
	{
		metric: v1alpha1.Metric{
			Name: "testapp",
			Provider: v1alpha1.MetricProvider{
				OPSMX: &v1alpha1.OPSMXMetric{
					User:                 "admin",
					GateUrl:              "https://opsmx.test.tst",
					Application:          "multiservice",
					BaselineStartTime:    "",
					CanaryStartTime:      "2022-08-10T13:15:00Z",
					EndTime:              "2022-08-10T13:45:10Z",
					GlobalMetricTemplate: "metricTemplate",
					Threshold: v1alpha1.OPSMXThreshold{
						Pass:     80,
						Marginal: 65,
					},
					Services: []v1alpha1.OPSMXService{
						{
							MetricScopeVariables:  "job_name",
							BaselineMetricScope:   "oes-sapor-br",
							CanaryMetricScope:     "oes-sapor-cr",
							MetricTemplateName:    "metricTemplate",
							MetricTemplateVersion: "1",
						},
						{
							MetricScopeVariables: "job_name",
							BaselineMetricScope:  "oes-platform-br",
							CanaryMetricScope:    "oes-platform-cr",
						},
					},
				},
			},
		},
		payloadRegisterCanary: `		{
			"application": "multiservice",
			"canaryConfig": {
			  "lifetimeHours": "0.5",
			  "canaryHealthCheckHandler": {
				"minimumCanaryResultScore": "65"
			  },
			  "canarySuccessCriteria": {
				"canaryResultScore": "80"
			  }
			},
			"canaryDeployments": [
			  {
				"canaryStartTimeMs": "1660137300000",
				"baselineStartTimeMs": "1660137300000",
				"canary": {
				  "metric": {
					"service1": {
					  "serviceGate": "gate1",
					  "job_name": "oes-sapor-cr",
					  "template":"metricTemplate",
					  "templateVersion":"1"
					},
					"service2": {
					  "serviceGate": "gate2",
					  "job_name": "oes-platform-cr",
					  "template":"metricTemplate"
					}
				  }
				},
				"baseline": {
				  "metric": {
					"service1": {
					  "serviceGate": "gate1",
					  "job_name": "oes-sapor-br",
					  "template":"metricTemplate",
					  "templateVersion":"1"
					},
					"service2": {
					  "serviceGate": "gate2",
					  "job_name": "oes-platform-br",
					  "template":"metricTemplate"
					}
				  }
				}
			  }
			]
		  }`,
		reportUrl: "https://opsmx.test.tst/ui/application/deploymentverification/multiservice/1424",
	},

	//Test case for multi-service feature along with logs+metrics analysis
	{
		metric: v1alpha1.Metric{
			Name: "testapp",
			Provider: v1alpha1.MetricProvider{
				OPSMX: &v1alpha1.OPSMXMetric{
					GateUrl:           "https://opsmx.test.tst",
					User:              "admin",
					Application:       "multiservice",
					BaselineStartTime: "",
					CanaryStartTime:   "2022-08-10T13:15:00Z",
					EndTime:           "2022-08-10T13:45:10Z",
					Threshold: v1alpha1.OPSMXThreshold{
						Pass:     80,
						Marginal: 65,
					},
					Services: []v1alpha1.OPSMXService{
						{
							MetricScopeVariables:  "job_name",
							BaselineMetricScope:   "oes-platform-br",
							CanaryMetricScope:     "oes-platform-cr",
							MetricTemplateName:    "metricTemplate",
							MetricTemplateVersion: "1",
						},
						{
							MetricScopeVariables:  "job_name",
							BaselineMetricScope:   "oes-sapor-br",
							CanaryMetricScope:     "oes-sapor-cr",
							MetricTemplateName:    "metricTemplate",
							MetricTemplateVersion: "1",
							LogScopeVariables:     "kubernetes.container_name",
							BaselineLogScope:      "oes-datascience-br",
							CanaryLogScope:        "oes-datascience-cr",
							LogTemplateName:       "logTemplate",
							LogTemplateVersion:    "1",
						},
					},
				},
			},
		},
		payloadRegisterCanary: `{
			"application": "multiservice",
			"canaryConfig": {
			  "lifetimeHours": "0.5",
			  "canaryHealthCheckHandler": {
				"minimumCanaryResultScore": "65"
			  },
			  "canarySuccessCriteria": {
				"canaryResultScore": "80"
			  }
			},
			"canaryDeployments": [
			  {
				"canaryStartTimeMs": "1660137300000",
				"baselineStartTimeMs": "1660137300000",
				"canary": {
				  "log": {
					"service2": {
					  "serviceGate": "gate2",
					  "kubernetes.container_name": "oes-datascience-cr",
					  "template":"logTemplate",
					  "templateVersion":"1"
					}
				  },
				  "metric": {
					"service1": {
					  "serviceGate": "gate1",
					  "job_name": "oes-platform-cr",
					  "template":"metricTemplate",
					  "templateVersion":"1"
					},
					"service2": {
					  "serviceGate": "gate2",
					  "job_name": "oes-sapor-cr",
					  "template":"metricTemplate",
					  "templateVersion":"1"
					}
				  }
				},
				"baseline": {
				  "log": {
					"service2": {
					  "serviceGate": "gate2",
					  "kubernetes.container_name": "oes-datascience-br",
					  "template":"logTemplate",
					  "templateVersion":"1"
					}
				  },
				  "metric": {
					"service1": {
					  "serviceGate": "gate1",
					  "job_name": "oes-platform-br",
					  "template":"metricTemplate",
					  "templateVersion":"1"
					},
					"service2": {
					  "serviceGate": "gate2",
					  "job_name": "oes-sapor-br",
					  "template":"metricTemplate",
					  "templateVersion":"1"
					}
				  }
				}
			  }
			]
		  }`,
		reportUrl: "https://opsmx.test.tst/ui/application/deploymentverification/multiservice/1424",
	},
	//Test case for 1 incorrect service and one correct
	{
		metric: v1alpha1.Metric{
			Name: "testapp",
			Provider: v1alpha1.MetricProvider{
				OPSMX: &v1alpha1.OPSMXMetric{
					GateUrl:           "https://opsmx.test.tst",
					User:              "admin",
					Application:       "multiservice",
					BaselineStartTime: "",
					CanaryStartTime:   "2022-08-10T13:15:00Z",
					EndTime:           "2022-08-10T13:45:10Z",
					Threshold: v1alpha1.OPSMXThreshold{
						Pass:     80,
						Marginal: 65,
					},
					Services: []v1alpha1.OPSMXService{
						{
							MetricScopeVariables: "job_name",
							BaselineMetricScope:  "oes-platform-br",
							CanaryMetricScope:    "oes-platform-cr",
							MetricTemplateName:   "metricTemplate",
						},
						{
							MetricScopeVariables: "job_name",
							BaselineMetricScope:  "oes-sapor-br",
							CanaryMetricScope:    "oes-sapor-cr",
							MetricTemplateName:   "metricTemplate",
							LogScopeVariables:    "kubernetes.container_name",
							BaselineLogScope:     "oes-datascience-br",
							CanaryLogScope:       "oes-datascience-cr",
							LogTemplateName:      "logTemplate",
						},
					},
				},
			},
		},
		payloadRegisterCanary: `{
			"application": "multiservice",
			"canaryConfig": {
			  "lifetimeHours": "0.5",
			  "canaryHealthCheckHandler": {
				"minimumCanaryResultScore": "65"
			  },
			  "canarySuccessCriteria": {
				"canaryResultScore": "80"
			  }
			},
			"canaryDeployments": [
			  {
				"canaryStartTimeMs": "1660137300000",
				"baselineStartTimeMs": "1660137300000",
				"canary": {
				  "log": {
					"service2": {
					  "serviceGate": "gate2",
					  "kubernetes.container_name": "oes-datascience-cr",
					  "template":"logTemplate"
					}
				  },
				  "metric": {
					"service1": {
					  "serviceGate": "gate1",
					  "job_name": "oes-platform-cr",
					  "template":"metricTemplate"
					},
					"service2": {
					  "serviceGate": "gate2",
					  "job_name": "oes-sapor-cr",
					  "template":"metricTemplate"
					}
				  }
				},
				"baseline": {
				  "log": {
					"service2": {
					  "serviceGate": "gate2",
					  "kubernetes.container_name": "oes-datascience-br",
					  "template":"logTemplate"
					}
				  },
				  "metric": {
					"service1": {
					  "serviceGate": "gate1",
					  "job_name": "oes-platform-br",
					  "template":"metricTemplate"
					},
					"service2": {
					  "serviceGate": "gate2",
					  "job_name": "oes-sapor-br",
					  "template":"metricTemplate"
					}
				  }
				}
			  }
			]
		  }`,
		reportUrl: "https://opsmx.test.tst/ui/application/deploymentverification/multiservice/1424",
	},
}

var negativeTests = []struct {
	metric        v1alpha1.Metric
	expectedPhase v1alpha1.AnalysisPhase
	message       string
}{

	//Test case for no lifetimeHours, Baseline/Canary start time
	{
		metric: v1alpha1.Metric{
			Name: "testapp",
			Provider: v1alpha1.MetricProvider{
				OPSMX: &v1alpha1.OPSMXMetric{
					GateUrl:     "https://opsmx.test.tst",
					Application: "testapp",
					User:        "admin",
					Threshold: v1alpha1.OPSMXThreshold{
						Pass:     80,
						Marginal: 65,
					},
					Services: []v1alpha1.OPSMXService{
						{
							MetricScopeVariables: "job_name",
							BaselineMetricScope:  "oes-datascience-br",
							CanaryMetricScope:    "oes-datascience-cr",
							MetricTemplateName:   "metrictemplate",
						},
					},
				},
			},
		},
		expectedPhase: v1alpha1.AnalysisPhaseError,
		message:       "either provide lifetimehours or start time",
	},
	//Test case for Pass score less than marginal
	{
		metric: v1alpha1.Metric{
			Name: "testapp",
			Provider: v1alpha1.MetricProvider{
				OPSMX: &v1alpha1.OPSMXMetric{
					GateUrl:           "https://opsmx.test.tst",
					Application:       "testapp",
					User:              "admin",
					BaselineStartTime: "2022-08-02T13:15:00Z",
					CanaryStartTime:   "2022-08-02T13:15:00Z",
					LifetimeHours:     "0.05",
					Threshold: v1alpha1.OPSMXThreshold{
						Pass:     60,
						Marginal: 80,
					},
					Services: []v1alpha1.OPSMXService{
						{
							MetricScopeVariables: "job_name",
							BaselineMetricScope:  "oes-datascience-br",
							CanaryMetricScope:    "oes-datascience-cr",
							MetricTemplateName:   "metrictemplate",
						},
					},
				},
			},
		},
		expectedPhase: v1alpha1.AnalysisPhaseError,
		message:       "pass score cannot be less than marginal score",
	},
	//Test case for inappropriate time format
	{
		metric: v1alpha1.Metric{
			Name: "testapp",
			Provider: v1alpha1.MetricProvider{
				OPSMX: &v1alpha1.OPSMXMetric{
					GateUrl:           "https://opsmx.test.tst",
					Application:       "testapp",
					User:              "admin",
					BaselineStartTime: "2022-08-02T13:15:00Z",
					CanaryStartTime:   "2022-O8-02T13:15:00Z",
					LifetimeHours:     "0.05",
					Threshold: v1alpha1.OPSMXThreshold{
						Pass:     80,
						Marginal: 60,
					},
					Services: []v1alpha1.OPSMXService{
						{
							MetricScopeVariables: "job_name",
							BaselineMetricScope:  "oes-datascience-br",
							CanaryMetricScope:    "oes-datascience-cr",
							MetricTemplateName:   "metrictemplate",
						},
					},
				},
			},
		},
		expectedPhase: v1alpha1.AnalysisPhaseError,
		message:       "parsing time \"2022-O8-02T13:15:00Z\" as \"2006-01-02T15:04:05Z07:00\": cannot parse \"O8-02T13:15:00Z\" as \"01\"",
	},
	//Test case for no lifetimeHours & EndTime
	{
		metric: v1alpha1.Metric{
			Name: "testapp",
			Provider: v1alpha1.MetricProvider{
				OPSMX: &v1alpha1.OPSMXMetric{
					GateUrl:           "https://opsmx.test.tst",
					Application:       "testapp",
					User:              "admin",
					BaselineStartTime: "2022-08-02T13:15:00Z",
					CanaryStartTime:   "2022-08-02T13:15:00Z",
					Threshold: v1alpha1.OPSMXThreshold{
						Pass:     80,
						Marginal: 60,
					},
					Services: []v1alpha1.OPSMXService{
						{
							MetricScopeVariables: "job_name",
							BaselineMetricScope:  "oes-datascience-br",
							CanaryMetricScope:    "oes-datascience-cr",
							MetricTemplateName:   "metrictemplate",
						},
					},
				},
			},
		},
		expectedPhase: v1alpha1.AnalysisPhaseError,
		message:       "either provide lifetimehours or end time",
	},
	//Test case for missing in baseline/canary for log analysis
	{
		metric: v1alpha1.Metric{
			Name: "testapp",
			Provider: v1alpha1.MetricProvider{
				OPSMX: &v1alpha1.OPSMXMetric{
					GateUrl:           "https://ds312.isd-dev.opsmx.net/",
					User:              "admin",
					Application:       "multiservice",
					BaselineStartTime: "",
					CanaryStartTime:   "2022-08-10T13:15:00Z",
					EndTime:           "2022-08-10T13:45:10Z",
					Threshold: v1alpha1.OPSMXThreshold{
						Pass:     80,
						Marginal: 65,
					},
					Services: []v1alpha1.OPSMXService{
						{
							MetricScopeVariables: "job_name",
							BaselineMetricScope:  "oes-platform-br",
							CanaryMetricScope:    "oes-platform-cr",
							MetricTemplateName:   "metrictemplate",
						},
						{
							MetricScopeVariables: "job_name",
							BaselineMetricScope:  "oes-sapor-br",
							CanaryMetricScope:    "oes-sapor-cr",
							MetricTemplateName:   "metrictemplate",
							LogScopeVariables:    "kubernetes.container_name",
							BaselineLogScope:     "",
							CanaryLogScope:       "",
							LogTemplateName:      "logtemplate",
						},
					},
				},
			},
		},

		expectedPhase: v1alpha1.AnalysisPhaseError,
		message:       "missing baseline/canary for log analysis",
	},
	//Test case for missing in baseline/canary for metric analysis
	{
		metric: v1alpha1.Metric{
			Name: "testapp",
			Provider: v1alpha1.MetricProvider{
				OPSMX: &v1alpha1.OPSMXMetric{
					GateUrl:           "https://ds312.isd-dev.opsmx.net/",
					User:              "admin",
					Application:       "multiservice",
					BaselineStartTime: "",
					CanaryStartTime:   "2022-08-10T13:15:00Z",
					EndTime:           "2022-08-10T13:45:10Z",
					Threshold: v1alpha1.OPSMXThreshold{
						Pass:     80,
						Marginal: 65,
					},
					Services: []v1alpha1.OPSMXService{
						{
							MetricScopeVariables: "job_name",
							BaselineMetricScope:  "",
							CanaryMetricScope:    "",
							MetricTemplateName:   "metrictemplate",
						},
						{
							MetricScopeVariables: "job_name",
							BaselineMetricScope:  "oes-sapor-br",
							CanaryMetricScope:    "oes-sapor-cr",
							MetricTemplateName:   "metrictemplate",
							LogScopeVariables:    "kubernetes.container_name",
							BaselineLogScope:     "baseline",
							CanaryLogScope:       "canary",
							LogTemplateName:      "logtemplate",
						},
					},
				},
			},
		},

		expectedPhase: v1alpha1.AnalysisPhaseError,
		message:       "missing baseline/canary for metric analysis",
	},
	//Test case for mismatch in log scope variables and baseline/canary log scope
	{
		metric: v1alpha1.Metric{
			Name: "testapp",
			Provider: v1alpha1.MetricProvider{
				OPSMX: &v1alpha1.OPSMXMetric{
					GateUrl:           "https://ds312.isd-dev.opsmx.net/",
					User:              "admin",
					Application:       "multiservice",
					BaselineStartTime: "",
					CanaryStartTime:   "2022-08-10T13:15:00Z",
					EndTime:           "2022-08-10T13:45:10Z",
					Threshold: v1alpha1.OPSMXThreshold{
						Pass:     80,
						Marginal: 65,
					},
					Services: []v1alpha1.OPSMXService{
						{
							MetricScopeVariables: "job_name",
							BaselineMetricScope:  "oes-platform-br",
							CanaryMetricScope:    "oes-platform-cr",
							MetricTemplateName:   "metrictemplate",
						},
						{
							MetricScopeVariables: "job_name",
							BaselineMetricScope:  "oes-sapor-br",
							CanaryMetricScope:    "oes-sapor-cr",
							MetricTemplateName:   "metrictemplate",
							LogScopeVariables:    "kubernetes.container_name,kubernetes.pod",
							BaselineLogScope:     "oes-datascience-br",
							CanaryLogScope:       "oes-datascience-cr",
							LogTemplateName:      "logtemplate",
						},
					},
				},
			},
		},

		expectedPhase: v1alpha1.AnalysisPhaseError,
		message:       "mismatch in number of log scope variables and baseline/canary log scope",
	},

	//Test case for mismatch in metric scope variables and baseline/canary metric scope
	{
		metric: v1alpha1.Metric{
			Name: "testapp",
			Provider: v1alpha1.MetricProvider{
				OPSMX: &v1alpha1.OPSMXMetric{
					GateUrl:           "https://ds312.isd-dev.opsmx.net/",
					User:              "admin",
					Application:       "multiservice",
					BaselineStartTime: "",
					CanaryStartTime:   "2022-08-10T13:15:00Z",
					EndTime:           "2022-08-10T13:45:10Z",
					Threshold: v1alpha1.OPSMXThreshold{
						Pass:     80,
						Marginal: 65,
					},
					Services: []v1alpha1.OPSMXService{
						{
							MetricScopeVariables: "job_name,job123",
							BaselineMetricScope:  "oes-platform-br",
							CanaryMetricScope:    "oes-platform-cr",
							MetricTemplateName:   "metrictemplate",
						},
						{
							MetricScopeVariables: "job_name",
							BaselineMetricScope:  "oes-sapor-br",
							CanaryMetricScope:    "oes-sapor-cr",
							MetricTemplateName:   "metrictemplate",
							LogScopeVariables:    "kubernetes.container_name",
							BaselineLogScope:     "oes-datascience-br",
							CanaryLogScope:       "oes-datascience-cr",
							LogTemplateName:      "logtemplate",
						},
					},
				},
			},
		},

		expectedPhase: v1alpha1.AnalysisPhaseError,
		message:       "mismatch in number of metric scope variables and baseline/canary metric scope",
	},
	//Test case for when end time is less than start time
	{
		metric: v1alpha1.Metric{
			Name: "testapp",
			Provider: v1alpha1.MetricProvider{
				OPSMX: &v1alpha1.OPSMXMetric{
					GateUrl:           "https://opsmx.test.tst",
					Application:       "testapp",
					BaselineStartTime: "2022-08-02T13:15:00Z",
					CanaryStartTime:   "2022-08-02T13:15:00Z",
					EndTime:           "2022-08-02T12:45:00Z",
					Threshold: v1alpha1.OPSMXThreshold{
						Pass:     80,
						Marginal: 60,
					},
					Services: []v1alpha1.OPSMXService{
						{
							MetricScopeVariables: "job_name",
							BaselineMetricScope:  "oes-datascience-br",
							CanaryMetricScope:    "oes-datascience-cr",
							MetricTemplateName:   "metrictemplate",
						},
					},
				},
			},
		},

		expectedPhase: v1alpha1.AnalysisPhaseError,
		message:       "start time cannot be greater than end time",
	},
}

const (
	endpointRegisterCanary    = "https://opsmx.test.tst/autopilot/api/v5/registerCanary"
	endpointCheckCanaryStatus = "https://opsmx.test.tst/autopilot/canaries/1424"
)

func TestRunSucessCases(t *testing.T) {
	// Test Cases
	for _, test := range successfulTests {
		e := log.NewEntry(log.New())
		c := NewTestClient(func(req *http.Request) (*http.Response, error) {
			assert.Equal(t, endpointRegisterCanary, req.URL.String())

			body, err := ioutil.ReadAll(req.Body)
			if err != nil {
				panic(err)
			}
			bodyI := map[string]interface{}{}
			err = json.Unmarshal(body, &bodyI)
			if err != nil {
				panic(err)
			}
			expectedBodyI := map[string]interface{}{}
			err = json.Unmarshal([]byte(test.payloadRegisterCanary), &expectedBodyI)
			if err != nil {
				panic(err)
			}
			assert.Equal(t, expectedBodyI, bodyI)
			return &http.Response{
				StatusCode: 200,
				Body: ioutil.NopCloser(bytes.NewBufferString(`
				{
					"canaryId": 1424
				}
				`)),
				// Must be set to non-nil value or it panics
				Header: make(http.Header),
			}, nil
		})
		provider := NewOPSMXProvider(*e, c)
		measurement := provider.Run(newAnalysisRun(), test.metric)
		assert.NotNil(t, measurement.StartedAt)
		assert.Equal(t, "1424", measurement.Metadata["canaryId"])
		assert.Equal(t, fmt.Sprintf("Report Url: %s", test.reportUrl), measurement.Metadata["reportUrl"])
		assert.Equal(t, v1alpha1.AnalysisPhaseRunning, measurement.Phase)
	}
}

func TestResumeSucessCases(t *testing.T) {

	for _, test := range successfulTests {
		e := log.NewEntry(log.New())
		c := NewTestClient(func(req *http.Request) (*http.Response, error) {
			assert.Equal(t, endpointCheckCanaryStatus, req.URL.String())

			return &http.Response{
				StatusCode: 200,
				Body: ioutil.NopCloser(bytes.NewBufferString(`
				{
					"owner": "admin",
					"application": "testapp",
					"canaryResult": {
						"duration": "0 seconds",
						"lastUpdated": "2022-09-02 10:02:18.504",
						"canaryReportURL": "https://opsmx.test.tst/ui/application/deploymentverification/testapp/1424",
						"overallScore": 100,
						"overallResult": "HEALTHY",
						"message": "Canary Is HEALTHY",
						"errors": []
					},
					"launchedDate": "2022-09-02 10:02:18.504",
					"canaryConfig": {
						"combinedCanaryResultStrategy": "LOWEST",
						"minimumCanaryResultScore": 65.0,
						"name": "admin",
						"lifetimeMinutes": 30,
						"canaryAnalysisIntervalMins": 30,
						"maximumCanaryResultScore": 80.0
					},
					"id": "1424",
					"services": [],
					"status": {
						"complete": false,
						"status": "COMPLETED"
					}}
				`)),
				// Must be set to non-nil value or it panics
				Header: make(http.Header),
			}, nil
		})

		provider := NewOPSMXProvider(*e, c)

		mapMetadata := make(map[string]string)
		mapMetadata["canaryId"] = "1424"
		mapMetadata["reportUrl"] = fmt.Sprintf("Report Url: %s", test.reportUrl)

		measurement := v1alpha1.Measurement{
			Metadata: mapMetadata,
			Phase:    v1alpha1.AnalysisPhaseRunning,
		}
		measurement = provider.Resume(newAnalysisRun(), test.metric, measurement)
		assert.Equal(t, "100", measurement.Value)
		assert.NotNil(t, measurement.FinishedAt)
		assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, measurement.Phase)
	}
}

func TestFailNoLogsConfiguredStillPassedInService(t *testing.T) {
	e := log.NewEntry(log.New())
	c := NewTestClient(func(req *http.Request) (*http.Response, error) {
		assert.Equal(t, endpointRegisterCanary, req.URL.String())

		return &http.Response{
			StatusCode: 404,
			Body: ioutil.NopCloser(bytes.NewBufferString(`
			{
				"timestamp": 1.662356583464E12,
				"status": 404.0,
				"error": "Not Found",
				"message": "Log template not configured for a service : service1",
				"path": "/autopilot/api/v3/registerCanary"
			}
			`)),
			// Must be set to non-nil value or it panics
			Header: make(http.Header),
		}, nil
	})

	metric := v1alpha1.Metric{
		Name: "testapp",
		Provider: v1alpha1.MetricProvider{
			OPSMX: &v1alpha1.OPSMXMetric{
				GateUrl:           "https://opsmx.test.tst",
				Application:       "multiservice",
				User:              "admin",
				BaselineStartTime: "",
				CanaryStartTime:   "2022-08-10T13:15:00Z",
				EndTime:           "2022-08-10T13:45:10Z",
				Threshold: v1alpha1.OPSMXThreshold{
					Pass:     80,
					Marginal: 65,
				},
				Services: []v1alpha1.OPSMXService{
					{
						LogScopeVariables:    "kubernetes.container_name",
						BaselineLogScope:     "oes-datascience-br",
						CanaryLogScope:       "oes-datascience-cr",
						LogTemplateName:      "logtemplate",
						MetricScopeVariables: "job_name",
						BaselineMetricScope:  "oes-sapor-br",
						CanaryMetricScope:    "oes-sapor-cr",
						MetricTemplateName:   "metrictemplate",
					},
				},
			},
		},
	}

	provider := NewOPSMXProvider(*e, c)
	measurement := provider.Run(newAnalysisRun(), metric)
	assert.NotNil(t, measurement.StartedAt)
	assert.NotNil(t, measurement.FinishedAt)
	assert.Equal(t, "Error: Not Found\nMessage: Log template not configured for a service : service1", measurement.Message)
	assert.Equal(t, v1alpha1.AnalysisPhaseError, measurement.Phase)

}

func TestIncorrectApplicationName(t *testing.T) {
	e := log.NewEntry(log.New())
	c := NewTestClient(func(req *http.Request) (*http.Response, error) {
		assert.Equal(t, endpointRegisterCanary, req.URL.String())

		return &http.Response{
			StatusCode: 500,
			Body: ioutil.NopCloser(bytes.NewBufferString(`
			{
				"timestamp": 1662442034995,
				"status": 500,
				"error": "Internal Server Error",
				"exception": "feign.FeignException$NotFound",
				"message": "Application not found with name testap"
			}
			`)),
			Header: make(http.Header),
		}, nil
	})

	metric := v1alpha1.Metric{
		Name: "testapp",
		Provider: v1alpha1.MetricProvider{
			OPSMX: &v1alpha1.OPSMXMetric{
				GateUrl:           "https://opsmx.test.tst",
				Application:       "testap",
				User:              "admin",
				BaselineStartTime: "2022-08-02T13:15:00Z",
				CanaryStartTime:   "2022-08-02T13:15:00Z",
				LifetimeHours:     "0.05",
				Threshold: v1alpha1.OPSMXThreshold{
					Pass:     80,
					Marginal: 60,
				},
				Services: []v1alpha1.OPSMXService{
					{
						MetricScopeVariables: "job_name",
						BaselineMetricScope:  "oes-datascience-br",
						CanaryMetricScope:    "oes-datascience-cr",
						MetricTemplateName:   "metrictemplate",
					},
				},
			},
		},
	}
	provider := NewOPSMXProvider(*e, c)
	measurement := provider.Run(newAnalysisRun(), metric)
	assert.NotNil(t, measurement.StartedAt)
	assert.NotNil(t, measurement.FinishedAt)
	assert.Equal(t, "Error: Internal Server Error\nMessage: Application not found with name testap", measurement.Message)
	assert.Equal(t, v1alpha1.AnalysisPhaseError, measurement.Phase)

}

func TestIncorrectGateURL(t *testing.T) {
	e := log.NewEntry(log.New())
	c := NewTestClient(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 404,
			Header:     make(http.Header),
		}, errors.New("Post \"https://opsmx.invalidurl.tst\": dial tcp: lookup https://opsmx.invalidurl.tst: no such host")
	})

	metric := v1alpha1.Metric{
		Name: "testapp",
		Provider: v1alpha1.MetricProvider{
			OPSMX: &v1alpha1.OPSMXMetric{
				GateUrl:           "https://opsmx.invalidurl.tst",
				Application:       "testapp",
				User:              "admin",
				BaselineStartTime: "2022-08-02T13:15:00Z",
				CanaryStartTime:   "2022-08-02T13:15:00Z",
				LifetimeHours:     "0.05",
				Threshold: v1alpha1.OPSMXThreshold{
					Pass:     80,
					Marginal: 60,
				},
				Services: []v1alpha1.OPSMXService{
					{
						MetricScopeVariables: "job_name",
						BaselineMetricScope:  "oes-datascience-br",
						CanaryMetricScope:    "oes-datascience-cr",
						MetricTemplateName:   "metrictemplate",
					},
				},
			},
		},
	}
	provider := NewOPSMXProvider(*e, c)
	measurement := provider.Run(newAnalysisRun(), metric)
	assert.NotNil(t, measurement.StartedAt)
	assert.NotNil(t, measurement.FinishedAt)
	assert.Equal(t, v1alpha1.AnalysisPhaseError, measurement.Phase)

}

func TestNoUserDefined(t *testing.T) {
	e := log.NewEntry(log.New())
	c := NewTestClient(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 500,
			Body: ioutil.NopCloser(bytes.NewBufferString(`
			{
				"timestamp": 1662442034995,
				"status": 500,
				"error": "Internal Server Error",
				"exception": "feign.FeignException$NotFound",
				"message": "message1"
			}
			`)),
			Header: make(http.Header),
		}, nil
	})

	metric := v1alpha1.Metric{
		Name: "testapp",
		Provider: v1alpha1.MetricProvider{
			OPSMX: &v1alpha1.OPSMXMetric{
				GateUrl:           "https://opsmx.test.tst",
				Application:       "testapp",
				BaselineStartTime: "2022-08-02T13:15:00Z",
				CanaryStartTime:   "2022-08-02T13:15:00Z",
				LifetimeHours:     "0.05",
				Threshold: v1alpha1.OPSMXThreshold{
					Pass:     80,
					Marginal: 60,
				},
				Services: []v1alpha1.OPSMXService{
					{
						MetricScopeVariables: "job_name",
						BaselineMetricScope:  "oes-datascience-br",
						CanaryMetricScope:    "oes-datascience-cr",
						MetricTemplateName:   "metrictemplate",
					},
				},
			},
		},
	}
	provider := NewOPSMXProvider(*e, c)
	measurement := provider.Run(newAnalysisRun(), metric)
	assert.NotNil(t, measurement.StartedAt)
	assert.NotNil(t, measurement.FinishedAt)
	assert.Equal(t, "Error: Internal Server Error\nMessage: message1", measurement.Message)
	assert.Equal(t, v1alpha1.AnalysisPhaseError, measurement.Phase)

}

func TestIncorrectServiceName(t *testing.T) {
	e := log.NewEntry(log.New())
	c := NewTestClient(func(req *http.Request) (*http.Response, error) {
		assert.Equal(t, endpointCheckCanaryStatus, req.URL.String())

		return &http.Response{
			StatusCode: 200,
			Body: ioutil.NopCloser(bytes.NewBufferString(`
				{
					"owner": "admin",
					"application": "multiservice",
					"canaryResult": {
						"duration": "0 seconds",
						"lastUpdated": "2022-09-06 09:16:51.58",
						"canaryReportURL": "https://opsmx.test.tst/ui/application/deploymentverification/multiservice/1424",
						"overallScore": null,
						"overallResult": "HEALTHY",
						"message": "Canary Is HEALTHY",
						"errors": []
					},
					"launchedDate": "2022-09-06 09:16:51.539",
					"canaryConfig": {
						"combinedCanaryResultStrategy": "LOWEST",
						"minimumCanaryResultScore": 65.0,
						"name": "admin",
						"lifetimeMinutes": 30,
						"canaryAnalysisIntervalMins": 30,
						"maximumCanaryResultScore": 80.0
					},
					"id": "1424",
					"services": [],
					"status": {
						"complete": true,
						"status": "COMPLETED"
					}
				}
				`)),
			// Must be set to non-nil value or it panics
			Header: make(http.Header),
		}, nil
	})
	metric := v1alpha1.Metric{
		Name: "testapp",
		Provider: v1alpha1.MetricProvider{
			OPSMX: &v1alpha1.OPSMXMetric{
				GateUrl:           "https://opsmx.test.tst/",
				User:              "admin",
				Application:       "multiservice",
				BaselineStartTime: "",
				CanaryStartTime:   "2022-08-10T13:15:00Z",
				EndTime:           "2022-08-10T13:45:10Z",
				Threshold: v1alpha1.OPSMXThreshold{
					Pass:     80,
					Marginal: 65,
				},
				Services: []v1alpha1.OPSMXService{
					{
						MetricScopeVariables: "job_name",
						BaselineMetricScope:  "oes-datascience-br",
						CanaryMetricScope:    "oes-datascience-cr",
						MetricTemplateName:   "metrictemplate",
					},
				},
			},
		},
	}
	provider := NewOPSMXProvider(*e, c)

	mapMetadata := make(map[string]string)
	mapMetadata["canaryId"] = "1424"
	mapMetadata["reportUrl"] = "Report Url: https://opsmx.test.tst/ui/application/deploymentverification/multiservice/1424"

	measurement := v1alpha1.Measurement{
		Metadata: mapMetadata,
		Phase:    v1alpha1.AnalysisPhaseRunning,
	}
	measurement = provider.Resume(newAnalysisRun(), metric, measurement)
	assert.NotEqual(t, "100", measurement.Value)
	assert.NotNil(t, measurement.FinishedAt)
	assert.Equal(t, v1alpha1.AnalysisPhaseFailed, measurement.Phase)
}

func TestGenericNegativeTestsRun(t *testing.T) {
	for _, test := range negativeTests {
		e := log.NewEntry(log.New())
		c := NewTestClient(func(req *http.Request) (*http.Response, error) {
			assert.Equal(t, endpointRegisterCanary, req.URL.String())
			return &http.Response{
				StatusCode: 200,
				// Send response to be tested
				Body: ioutil.NopCloser(bytes.NewBufferString(`
				{
				}
				`)),
				// Must be set to non-nil value or it panics
				Header: make(http.Header),
			}, nil
		})
		provider := NewOPSMXProvider(*e, c)
		measurement := provider.Run(newAnalysisRun(), test.metric)
		assert.NotNil(t, measurement.StartedAt)
		assert.NotNil(t, measurement.FinishedAt)
		assert.Equal(t, test.message, measurement.Message)
		assert.Equal(t, v1alpha1.AnalysisPhaseError, measurement.Phase)
	}
}

func newAnalysisRun() *v1alpha1.AnalysisRun {
	return &v1alpha1.AnalysisRun{}
}

// RoundTripFunc .
type RoundTripFunc func(req *http.Request) (*http.Response, error)

// RoundTrip .
func (f RoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// NewTestClient returns *http.Client with Transport replaced to avoid making real calls
func NewTestClient(fn RoundTripFunc) http.Client {
	return http.Client{
		Transport: fn,
	}
}
