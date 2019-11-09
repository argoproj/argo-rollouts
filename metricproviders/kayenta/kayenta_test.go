package kayenta

import (
	"argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"net/http"
	"time"

	//"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	//v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"testing"
)

func newAnalysisRun() *v1alpha1.AnalysisRun {
	return &v1alpha1.AnalysisRun{}
}

//metrics:
//- name: mann-whitney
//kayenta:
//address: https://kayenta.example.com
//application: guestbook
//canaryConfigName: my-test
//thresholds:
//pass: 90
//marginal: 75
//scopes:
//- name: default
//controlScope:
//scope: app=guestbook and rollouts-pod-template-hash={{inputs.stable-hash}}
//step: 60
//start: "{{inputs.start-time}}"
//end: "{{inputs.end-time}}"
//experimentScope:
//scope: app=guestbook and rollouts-pod-template-hash={{inputs.canary-hash}}
//step: 60
//start: "{{inputs.start-time}}"
//end: "{{inputs.end-time}}"
func TestRunSuccessfully(t *testing.T) {
	e := log.Entry{}
	c := http.Client{
		Timeout: 15 * time.Second,
	}

	p := NewKayentaProvider(e, c)
	metric := v1alpha1.Metric{
		Name:             "mann-whitney",
		Provider: v1alpha1.MetricProvider{
			Kayenta: &v1alpha1.KayentaMetric{
				Address: "https://kayenta.example.oom",
				Application: "guestbook",
				CanaryConfigName: "my-test",
				Threshold: v1alpha1.KayentaThreshold{
					Pass:     90,
					Marginal: 75,
				},
				Scopes: []v1alpha1.KayentaScope{
					v1alpha1.KayentaScope{
						Name:            "default",
						ControlScope:    v1alpha1.ScopeDetail{
							Scope: "app=guestbook and rollouts-pod-template-hash={{inputs.stable-hash}}",
							Step: 60,
							StartTime: "{{inputs.start-time}}",
							EndTime: "{{inputs.end-time}}",
						},
						ExperimentScope: v1alpha1.ScopeDetail{
							Scope: "app=guestbook and rollouts-pod-template-hash={{inputs.canary-hash}}",
							Step: 60,
							StartTime: "{{inputs.start-time}}",
							EndTime: "{{inputs.end-time}}",
						},
					},
				},
			},
		},

	}

	run := newAnalysisRun()
	run.Spec.Arguments = append(run.Spec.Arguments, v1alpha1.Argument{Name:"start", Value:"2019-03-29T01:08:34Z"})
	run.Spec.Arguments = append(run.Spec.Arguments, v1alpha1.Argument{Name:"end", Value:"2019-03-29T01:38:34Z"})
	run.Spec.Arguments = append(run.Spec.Arguments, v1alpha1.Argument{Name:"stable-hash", Value:"xxxx"})
	run.Spec.Arguments = append(run.Spec.Arguments, v1alpha1.Argument{Name:"canary-hash", Value:"yyyy"})

	measurement := p.Run(run, metric)
	assert.NotNil(t, measurement.StartedAt)
	assert.Equal(t, "10", measurement.Value)
	assert.NotNil(t, measurement.FinishedAt)
	assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, measurement.Phase)

	measurement = p.Resume(newAnalysisRun(), metric, measurement)
	assert.NotNil(t, measurement.StartedAt)
	assert.Equal(t, "10", measurement.Value)
	assert.NotNil(t, measurement.FinishedAt)
	assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, measurement.Phase)
}