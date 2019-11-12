package kayenta

import (
	"bytes"
	"io/ioutil"
	"net/http"
	"testing"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func newAnalysisRun() *v1alpha1.AnalysisRun {
	return &v1alpha1.AnalysisRun{}
}

/*
spec:
  inputs:
  - name: start-time #2019-03-29T01:08:34Z
  - name: end-time   #2019-03-29T01:38:34Z
  - name: stable-hash  #xxxx
  - name: canary-hash  #yyyy
  metrics:
  - name: mann-whitney
    kayenta:
      address: https://kayenta.example.com
      application: guestbook
      canaryConfigName: my-test
      canaryConfigId:   11111
	  metricsAccountName: wavefront-prod
      configurationAccountName: intuit-kayenta
      storageAccountName:  intuit-kayenta
      thresholds:
        pass: 90
        marginal: 75
      scopes:
      - name: default
        controlScope:
          scope: app=guestbook and rollouts-pod-template-hash={{inputs.stable-hash}}
          step: 60
          start: "{{inputs.start-time}}"
          end: "{{inputs.end-time}}"
        experimentScope:
          scope: app=guestbook and rollouts-pod-template-hash={{inputs.canary-hash}}
          step: 60
          start: "{{inputs.start-time}}"
          end: "{{inputs.end-time}}"
*/
func buildMetric() v1alpha1.Metric {
	return v1alpha1.Metric{
		Name: "mann-whitney",
		Provider: v1alpha1.MetricProvider{
			Kayenta: &v1alpha1.KayentaMetric{
				Address:                  "https://kayenta.example.oom",
				Application:              "guestbook",
				CanaryConfigName:         "my-test",
				CanaryConfigId:           "11111",
				MetricsAccountName:       "wavefront-prod",
				ConfigurationAccountName: "intuit-kayenta",
				StorageAccountName:       "intuit-kayenta",
				Threshold: v1alpha1.KayentaThreshold{
					Pass:     90,
					Marginal: 75,
				},
				Scopes: []v1alpha1.KayentaScope{
					{
						Name: "default",
						ControlScope: v1alpha1.ScopeDetail{
							Scope:     "app=guestbook and rollouts-pod-template-hash={{inputs.stable-hash}}",
							Step:      60,
							Region:    "us-=west-2",
							StartTime: "{{inputs.start-time}}",
							EndTime:   "{{inputs.end-time}}",
						},
						ExperimentScope: v1alpha1.ScopeDetail{
							Scope:     "app=guestbook and rollouts-pod-template-hash={{inputs.canary-hash}}",
							Step:      60,
							Region:    "us-=west-2",
							StartTime: "{{inputs.start-time}}",
							EndTime:   "{{inputs.end-time}}",
						},
					},
				},
			},
		},
	}
}

//

func TestRunSuccessfully(t *testing.T) {
	e := log.NewEntry(log.New())
	c := NewTestClient(func(req *http.Request) *http.Response {

		assert.Equal(t, req.URL.String(), "https://kayenta.example.oom/canary/11111?application=guestbook&metricsAccountName=wavefront-prod&configurationAccountName=intuit-kayenta&storageAccountName=intuit-kayenta")

		body, _ := ioutil.ReadAll(req.Body)
		assert.Equal(t, string(body), `
							{
								"scopes": {
										"default":{"controlScope": {"scope":"app=guestbook and rollouts-pod-template-hash=xxxx","region":"us-=west-2","step":60,"start":"2019-03-29T01:08:34Z","end":"2019-03-29T01:38:34Z"}, "experimentScope": {"scope":"app=guestbook and rollouts-pod-template-hash=yyyy","region":"us-=west-2","step":60,"start":"2019-03-29T01:08:34Z","end":"2019-03-29T01:38:34Z"}}
								},
                                "thresholds" : {
                                    "pass": 90,
                                    "marginal": 75
                                }
                            }`)

		return &http.Response{
			StatusCode: 200,
			// Send response to be tested
			Body: ioutil.NopCloser(bytes.NewBufferString(`
			{
				"canaryExecutionId" : "01DS50WVHAWSTAQACJKB1VKDQB"
            }
			`)),
			// Must be set to non-nil value or it panics
			Header: make(http.Header),
		}
	})

	p := NewKayentaProvider(*e, c)
	metric := buildMetric()

	run := newAnalysisRun()
	run.Spec.Arguments = []v1alpha1.Argument{
		{Name: "start-time", Value: "2019-03-29T01:08:34Z"},
		{Name: "end-time", Value: "2019-03-29T01:38:34Z"},
		{Name: "stable-hash", Value: "xxxx"},
		{Name: "canary-hash", Value: "yyyy"},
	}

	measurement := p.Run(run, metric)

	assert.NotNil(t, measurement.StartedAt)
	assert.NotNil(t, measurement.ResumeAt)
	assert.Equal(t, "01DS50WVHAWSTAQACJKB1VKDQB", measurement.Metadata["canaryExecutionId"])
	assert.Equal(t, v1alpha1.AnalysisPhaseRunning, measurement.Phase)

	assert.Equal(t, "Kayenta", p.Type())
	assert.IsType(t, http.Client{}, NewHttpClient())

	assert.Equal(t, nil, p.GarbageCollect(run, metric, 0))

	measurement2 := p.Terminate(run, metric, measurement)
	assert.Equal(t, measurement, measurement2)
}

func TestRunBadResponse(t *testing.T) {
	e := log.Entry{}
	c := NewTestClient(func(req *http.Request) *http.Response {

		assert.Equal(t, req.URL.String(), "https://kayenta.example.oom/canary/11111?application=guestbook&metricsAccountName=wavefront-prod&configurationAccountName=intuit-kayenta&storageAccountName=intuit-kayenta")

		body, _ := ioutil.ReadAll(req.Body)
		assert.Equal(t, string(body), `
							{
								"scopes": {
										"default":{"controlScope": {"scope":"app=guestbook and rollouts-pod-template-hash=xxxx","region":"us-=west-2","step":60,"start":"2019-03-29T01:08:34Z","end":"2019-03-29T01:38:34Z"}, "experimentScope": {"scope":"app=guestbook and rollouts-pod-template-hash=yyyy","region":"us-=west-2","step":60,"start":"2019-03-29T01:08:34Z","end":"2019-03-29T01:38:34Z"}}
								},
                                "thresholds" : {
                                    "pass": 90,
                                    "marginal": 75
                                }
                            }`)

		return &http.Response{
			StatusCode: 500,
			// Send response to be tested
			//Body:       ioutil.NopCloser(bytes.NewBufferString(`
			//{
			//	"canaryExecutionId" : "01DS50WVHAWSTAQACJKB1VKDQB"
			//}
			//`)),
			// Must be set to non-nil value or it panics
			Header: make(http.Header),
		}
	})

	p := NewKayentaProvider(e, c)
	metric := buildMetric()

	run := newAnalysisRun()
	run.Spec.Arguments = []v1alpha1.Argument{
		{Name: "start-time", Value: "2019-03-29T01:08:34Z"},
		{Name: "end-time", Value: "2019-03-29T01:38:34Z"},
		{Name: "stable-hash", Value: "xxxx"},
		{Name: "canary-hash", Value: "yyyy"},
	}

	measurement := p.Run(run, metric)

	assert.NotNil(t, measurement.StartedAt)
	assert.Equal(t, v1alpha1.AnalysisPhaseError, measurement.Phase)

}

func TestRunEmptyExecutionId(t *testing.T) {
	e := log.Entry{}
	c := NewTestClient(func(req *http.Request) *http.Response {

		assert.Equal(t, req.URL.String(), "https://kayenta.example.oom/canary/11111?application=guestbook&metricsAccountName=wavefront-prod&configurationAccountName=intuit-kayenta&storageAccountName=intuit-kayenta")

		body, _ := ioutil.ReadAll(req.Body)
		assert.Equal(t, string(body), `
							{
								"scopes": {
										"default":{"controlScope": {"scope":"app=guestbook and rollouts-pod-template-hash=xxxx","region":"us-=west-2","step":60,"start":"2019-03-29T01:08:34Z","end":"2019-03-29T01:38:34Z"}, "experimentScope": {"scope":"app=guestbook and rollouts-pod-template-hash=yyyy","region":"us-=west-2","step":60,"start":"2019-03-29T01:08:34Z","end":"2019-03-29T01:38:34Z"}}
								},
                                "thresholds" : {
                                    "pass": 90,
                                    "marginal": 75
                                }
                            }`)

		return &http.Response{
			StatusCode: 200,
			// Send response to be tested
			Body: ioutil.NopCloser(bytes.NewBufferString(`
			{
				"canaryExecutionId" : ""
			}
			`)),
			// Must be set to non-nil value or it panics
			Header: make(http.Header),
		}
	})

	p := NewKayentaProvider(e, c)
	metric := buildMetric()

	run := newAnalysisRun()
	run.Spec.Arguments = []v1alpha1.Argument{
		{Name: "start-time", Value: "2019-03-29T01:08:34Z"},
		{Name: "end-time", Value: "2019-03-29T01:38:34Z"},
		{Name: "stable-hash", Value: "xxxx"},
		{Name: "canary-hash", Value: "yyyy"},
	}

	measurement := p.Run(run, metric)

	assert.NotNil(t, measurement.StartedAt)
	assert.Equal(t, v1alpha1.AnalysisPhaseError, measurement.Phase)

}

func TestResumeSuccessfully(t *testing.T) {
	e := log.Entry{}
	c := NewTestClient(func(req *http.Request) *http.Response {

		assert.Equal(t, req.URL.String(), "https://kayenta.example.oom/canary/01DS50WVHAWSTAQACJKB1VKDQB")

		return &http.Response{
			StatusCode: 200,
			//result.judgeResult.score.score
			Body: ioutil.NopCloser(bytes.NewBufferString(`
			{
				"result" : {
								"judgeResult": {
									"score": { "score": 100.0 }
								}
							}
            }
			`)),
			// Must be set to non-nil value or it panics
			Header: make(http.Header),
		}
	})

	p := NewKayentaProvider(e, c)

	metric := buildMetric()
	m := make(map[string]string)
	m["canaryExecutionId"] = "01DS50WVHAWSTAQACJKB1VKDQB"
	measurement := v1alpha1.Measurement{
		Metadata: m,
	}

	measurement = p.Resume(newAnalysisRun(), metric, measurement)

	assert.Equal(t, "100", measurement.Value)
	assert.NotNil(t, measurement.FinishedAt)
	assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, measurement.Phase)

}

func TestResumeBadResponse(t *testing.T) {
	e := log.Entry{}
	c := NewTestClient(func(req *http.Request) *http.Response {

		assert.Equal(t, req.URL.String(), "https://kayenta.example.oom/canary/01DS50WVHAWSTAQACJKB1VKDQB")

		return &http.Response{
			StatusCode: 500,
			// Must be set to non-nil value or it panics
			Header: make(http.Header),
		}
	})

	p := NewKayentaProvider(e, c)

	metric := buildMetric()
	m := make(map[string]string)
	m["canaryExecutionId"] = "01DS50WVHAWSTAQACJKB1VKDQB"
	measurement := v1alpha1.Measurement{
		Metadata: m,
	}

	measurement = p.Resume(newAnalysisRun(), metric, measurement)

	assert.Equal(t, v1alpha1.AnalysisPhaseError, measurement.Phase)

}

func TestResumeInvalidScore(t *testing.T) {
	e := log.Entry{}
	c := NewTestClient(func(req *http.Request) *http.Response {

		assert.Equal(t, req.URL.String(), "https://kayenta.example.oom/canary/01DS50WVHAWSTAQACJKB1VKDQB")

		return &http.Response{
			StatusCode: 200,
			//result.judgeResult.score.score
			Body: ioutil.NopCloser(bytes.NewBufferString(`
			{
				"result" : {
								"judgeResult": {
								}
							}
			}
			`)),
			// Must be set to non-nil value or it panics
			Header: make(http.Header),
		}
	})

	p := NewKayentaProvider(e, c)

	metric := buildMetric()
	m := make(map[string]string)
	m["canaryExecutionId"] = "01DS50WVHAWSTAQACJKB1VKDQB"
	measurement := v1alpha1.Measurement{
		Metadata: m,
	}

	measurement = p.Resume(newAnalysisRun(), metric, measurement)

	assert.Equal(t, v1alpha1.AnalysisPhaseError, measurement.Phase)

}

func TestResumeFailure(t *testing.T) {
	e := log.Entry{}
	c := NewTestClient(func(req *http.Request) *http.Response {

		assert.Equal(t, req.URL.String(), "https://kayenta.example.oom/canary/01DS50WVHAWSTAQACJKB1VKDQB")

		return &http.Response{
			StatusCode: 200,
			//result.judgeResult.score.score
			Body: ioutil.NopCloser(bytes.NewBufferString(`
			{
				"result" : {
								"judgeResult": {
									"score": { "score": 60.0 }
								}
							}
            }
			`)),
			// Must be set to non-nil value or it panics
			Header: make(http.Header),
		}
	})

	p := NewKayentaProvider(e, c)
	metric := buildMetric()
	m := make(map[string]string)
	m["canaryExecutionId"] = "01DS50WVHAWSTAQACJKB1VKDQB"
	measurement := v1alpha1.Measurement{
		Metadata: m,
	}

	measurement = p.Resume(newAnalysisRun(), metric, measurement)
	assert.Equal(t, "60", measurement.Value)
	assert.NotNil(t, measurement.FinishedAt)
	assert.Equal(t, v1alpha1.AnalysisPhaseFailed, measurement.Phase)

}

func TestResumeInconclusive(t *testing.T) {
	e := log.Entry{}
	c := NewTestClient(func(req *http.Request) *http.Response {

		assert.Equal(t, req.URL.String(), "https://kayenta.example.oom/canary/01DS50WVHAWSTAQACJKB1VKDQB")

		return &http.Response{
			StatusCode: 200,
			//result.judgeResult.score.score
			Body: ioutil.NopCloser(bytes.NewBufferString(`
			{
				"result" : {
								"judgeResult": {
									"score": { "score": 80.0}
								}
							}
            }
			`)),
			// Must be set to non-nil value or it panics
			Header: make(http.Header),
		}
	})

	p := NewKayentaProvider(e, c)
	metric := buildMetric()
	m := make(map[string]string)
	m["canaryExecutionId"] = "01DS50WVHAWSTAQACJKB1VKDQB"
	measurement := v1alpha1.Measurement{
		Metadata: m,
	}

	measurement = p.Resume(newAnalysisRun(), metric, measurement)
	assert.Equal(t, "80", measurement.Value)
	assert.NotNil(t, measurement.FinishedAt)
	assert.Equal(t, v1alpha1.AnalysisPhaseInconclusive, measurement.Phase)

}

// RoundTripFunc .
type RoundTripFunc func(req *http.Request) *http.Response

// RoundTrip .
func (f RoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req), nil
}

//NewTestClient returns *http.Client with Transport replaced to avoid making real calls
func NewTestClient(fn RoundTripFunc) http.Client {
	return http.Client{
		Transport: fn,
	}
}
