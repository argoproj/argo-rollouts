package kayenta

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
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
				CanaryConfigName:         "ValidationCanaryConfigTest3",
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
							Scope:  "app=guestbook and rollouts-pod-template-hash=xxxx",
							Step:   60,
							Region: "us-=west-2",
							Start:  "2019-03-29T01:08:34Z",
							End:    "2019-03-29T01:38:34Z",
						},
						ExperimentScope: v1alpha1.ScopeDetail{
							Scope:  "app=guestbook and rollouts-pod-template-hash=yyyy",
							Step:   60,
							Region: "us-=west-2",
							Start:  "2019-03-29T01:08:34Z",
							End:    "2019-03-29T01:38:34Z",
						},
					},
				},
			},
		},
	}
}

const (
	configIdLookupResponse = `
[
  {
    "id": "69a6c352-7033-4ca1-858b-02497e5cc672",
    "name": "ValidationCanaryConfigWF",
    "updatedTimestamp": 1556747152000,
    "updatedTimestampIso": "2019-05-01T21:45:52Z",
    "applications": [
      "guestbook"
    ]
  },
  {
    "id": "99d93685-4f70-4e8b-aa6d-6c47bdb4ab94",
    "name": "ValidationCanaryConfigTest",
    "updatedTimestamp": 1542305351000,
    "updatedTimestampIso": "2018-11-15T18:09:11Z",
    "applications": [
      "guestbook"
    ]
  },
  {
    "id": "bf9795c0-170f-403e-bb7f-fef3f9a44d42",
    "name": "ValidationCanaryConfigTest3",
    "updatedTimestamp": 1553707842000,
    "updatedTimestampIso": "2019-03-27T17:30:42Z",
    "applications": [
      "guestbook"
    ]
  },
  {
    "id": "872bb4d3-3309-48e6-9203-2a456a5c2ce7",
    "name": "ValidationCanaryConfigTest2",
    "updatedTimestamp": 1542306335000,
    "updatedTimestampIso": "2018-11-15T18:25:35Z",
    "applications": [
      "guestbook"
    ]
  }
]
`
	lookupURL = "https://kayenta.example.oom/canaryConfig?application=guestbook&configurationAccountName=intuit-kayenta"
	jobURL    = "https://kayenta.example.oom/canary/bf9795c0-170f-403e-bb7f-fef3f9a44d42?application=guestbook&metricsAccountName=wavefront-prod&configurationAccountName=intuit-kayenta&storageAccountName=intuit-kayenta"
)

const expectedBody = `{	
	"scopes": {
		"default":{
			"controlScope": {
				"scope":"app=guestbook and rollouts-pod-template-hash=xxxx",
				"region":"us-=west-2",
				"step":60,"start":"2019-03-29T01:08:34Z","end":"2019-03-29T01:38:34Z"}, "experimentScope": {"scope":"app=guestbook and rollouts-pod-template-hash=yyyy","region":"us-=west-2","step":60,"start":"2019-03-29T01:08:34Z","end":"2019-03-29T01:38:34Z"}}	
	},	
	"thresholds" : {	
		"pass": 90,	
		"marginal": 75	
	}	
}`

func TestRunSuccessfully(t *testing.T) {
	e := log.NewEntry(log.New())
	c := NewTestClient(func(req *http.Request) *http.Response {
		if req.URL.String() == jobURL {
			assert.Equal(t, req.URL.String(), jobURL)
			body, err := io.ReadAll(req.Body)
			if err != nil {
				panic(err)
			}
			bodyI := map[string]interface{}{}
			err = json.Unmarshal(body, &bodyI)
			if err != nil {
				panic(err)
			}
			expectedBodyI := map[string]interface{}{}
			err = json.Unmarshal([]byte(expectedBody), &expectedBodyI)
			if err != nil {
				panic(err)
			}
			assert.Equal(t, expectedBodyI, bodyI)
			return &http.Response{
				StatusCode: 200,
				// Send response to be tested
				Body: io.NopCloser(bytes.NewBufferString(`
			{
				"canaryExecutionId" : "01DS50WVHAWSTAQACJKB1VKDQB"
            }
			`)),
				// Must be set to non-nil value or it panics
				Header: make(http.Header),
			}
		} else {
			url := req.URL.String()
			assert.Equal(t, url, lookupURL)

			return &http.Response{
				StatusCode: 200,
				// Send response to be tested
				Body: io.NopCloser(bytes.NewBufferString(configIdLookupResponse)),
				// Must be set to non-nil value or it panics
				Header: make(http.Header),
			}
		}
	})

	p := NewKayentaProvider(*e, c)
	metric := buildMetric()

	stableHash := "xxxx"
	canaryHash := "yyyy"
	startTime := "2019-03-29T01:08:34Z"
	endTime := "2019-03-29T01:38:34Z"
	run := newAnalysisRun()
	run.Spec.Args = []v1alpha1.Argument{
		{Name: "start-time", Value: &startTime},
		{Name: "end-time", Value: &endTime},
		{Name: "stable-hash", Value: &stableHash},
		{Name: "canary-hash", Value: &canaryHash},
	}
	metricsMetadata := p.GetMetadata(metric)
	assert.Nil(t, metricsMetadata)

	measurement := p.Run(run, metric)

	assert.NotNil(t, measurement.StartedAt)
	assert.NotNil(t, measurement.ResumeAt)
	assert.Nil(t, measurement.FinishedAt)
	assert.Equal(t, "01DS50WVHAWSTAQACJKB1VKDQB", measurement.Metadata["canaryExecutionId"])
	assert.Equal(t, v1alpha1.AnalysisPhaseRunning, measurement.Phase)

	assert.Equal(t, "Kayenta", p.Type())
	assert.IsType(t, http.Client{}, NewHttpClient())

	assert.Equal(t, nil, p.GarbageCollect(run, metric, 0))

	measurement2 := p.Terminate(run, metric, measurement)
	assert.Equal(t, measurement, measurement2)
}

func TestRunBadJobResponse(t *testing.T) {
	e := log.Entry{}
	c := NewTestClient(func(req *http.Request) *http.Response {
		if req.URL.String() == jobURL {
			assert.Equal(t, req.URL.String(), jobURL)
			return &http.Response{
				StatusCode: 500,
				// Send response to be tested
				//	Body: io.NopCloser(bytes.NewBufferString(`
				// {
				//	"canaryExecutionId" : "01DS50WVHAWSTAQACJKB1VKDQB"
				// }
				// `)),
				// Must be set to non-nil value or it panics
				Header: make(http.Header),
			}
		} else {
			url := req.URL.String()
			assert.Equal(t, url, lookupURL)

			return &http.Response{
				StatusCode: 200,
				// Send response to be tested
				Body: io.NopCloser(bytes.NewBufferString(configIdLookupResponse)),
				// Must be set to non-nil value or it panics
				Header: make(http.Header),
			}
		}
	})

	p := NewKayentaProvider(e, c)
	metric := buildMetric()

	stableHash := "xxxx"
	canaryHash := "yyyy"
	startTime := "2019-03-29T01:08:34Z"
	endTime := "2019-03-29T01:38:34Z"
	run := newAnalysisRun()
	run.Spec.Args = []v1alpha1.Argument{
		{Name: "start-time", Value: &startTime},
		{Name: "end-time", Value: &endTime},
		{Name: "stable-hash", Value: &stableHash},
		{Name: "canary-hash", Value: &canaryHash},
	}
	metricsMetadata := p.GetMetadata(metric)
	assert.Nil(t, metricsMetadata)

	measurement := p.Run(run, metric)

	assert.NotNil(t, measurement.StartedAt)
	assert.Equal(t, v1alpha1.AnalysisPhaseError, measurement.Phase)

}

func TestRunBadLookupResponse(t *testing.T) {
	e := log.Entry{}
	c := NewTestClient(func(req *http.Request) *http.Response {
		if req.URL.String() == jobURL {
			assert.Equal(t, req.URL.String(), jobURL)

			body, _ := io.ReadAll(req.Body)
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
				//	Body: io.NopCloser(bytes.NewBufferString(`
				// {
				//	"canaryExecutionId" : "01DS50WVHAWSTAQACJKB1VKDQB"
				// }
				// `)),
				// Must be set to non-nil value or it panics
				Header: make(http.Header),
			}
		} else {
			url := req.URL.String()
			assert.Equal(t, url, lookupURL)

			return &http.Response{
				StatusCode: 500,
				// Send response to be tested
				// Body: io.NopCloser(bytes.NewBufferString(configIdLookupResponse)),
				// Must be set to non-nil value or it panics
				Header: make(http.Header),
			}
		}
	})

	p := NewKayentaProvider(e, c)
	metric := buildMetric()

	stableHash := "xxxx"
	canaryHash := "yyyy"
	startTime := "2019-03-29T01:08:34Z"
	endTime := "2019-03-29T01:38:34Z"
	run := newAnalysisRun()
	run.Spec.Args = []v1alpha1.Argument{
		{Name: "start-time", Value: &startTime},
		{Name: "end-time", Value: &endTime},
		{Name: "stable-hash", Value: &stableHash},
		{Name: "canary-hash", Value: &canaryHash},
	}

	measurement := p.Run(run, metric)

	assert.NotNil(t, measurement.StartedAt)
	assert.Equal(t, v1alpha1.AnalysisPhaseError, measurement.Phase)

}

func TestRunInvalidLookupResponse(t *testing.T) {
	e := log.Entry{}
	c := NewTestClient(func(req *http.Request) *http.Response {
		if req.URL.String() == jobURL {
			assert.Equal(t, req.URL.String(), jobURL)

			body, _ := io.ReadAll(req.Body)
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
				//	Body: io.NopCloser(bytes.NewBufferString(`
				// {
				//	"canaryExecutionId" : "01DS50WVHAWSTAQACJKB1VKDQB"
				// }
				// `)),
				// Must be set to non-nil value or it panics
				Header: make(http.Header),
			}
		} else {
			url := req.URL.String()
			assert.Equal(t, url, lookupURL)

			return &http.Response{
				StatusCode: 200,
				// Send response to be tested
				Body: io.NopCloser(bytes.NewBufferString(`{"this is bad": "yee"}`)),
				// Must be set to non-nil value or it panics
				Header: make(http.Header),
			}
		}
	})

	p := NewKayentaProvider(e, c)
	metric := buildMetric()

	stableHash := "xxxx"
	canaryHash := "yyyy"
	startTime := "2019-03-29T01:08:34Z"
	endTime := "2019-03-29T01:38:34Z"
	run := newAnalysisRun()
	run.Spec.Args = []v1alpha1.Argument{
		{Name: "start-time", Value: &startTime},
		{Name: "end-time", Value: &endTime},
		{Name: "stable-hash", Value: &stableHash},
		{Name: "canary-hash", Value: &canaryHash},
	}

	measurement := p.Run(run, metric)

	assert.NotNil(t, measurement.StartedAt)
	assert.Equal(t, v1alpha1.AnalysisPhaseError, measurement.Phase)

}

func TestRunEmptyExecutionId(t *testing.T) {
	e := log.Entry{}
	c := NewTestClient(func(req *http.Request) *http.Response {
		if req.URL.String() == jobURL {
			assert.Equal(t, req.URL.String(), jobURL)
			return &http.Response{
				StatusCode: 200,
				// Send response to be tested
				Body: io.NopCloser(bytes.NewBufferString(`
			{
				"canaryExecutionId" : ""
            }
			`)),
				// Must be set to non-nil value or it panics
				Header: make(http.Header),
			}
		} else {
			url := req.URL.String()
			assert.Equal(t, url, lookupURL)

			return &http.Response{
				StatusCode: 200,
				// Send response to be tested
				Body: io.NopCloser(bytes.NewBufferString(configIdLookupResponse)),
				// Must be set to non-nil value or it panics
				Header: make(http.Header),
			}
		}
	})

	p := NewKayentaProvider(e, c)
	metric := buildMetric()

	stableHash := "xxxx"
	canaryHash := "yyyy"
	startTime := "2019-03-29T01:08:34Z"
	endTime := "2019-03-29T01:38:34Z"
	run := newAnalysisRun()
	run.Spec.Args = []v1alpha1.Argument{
		{Name: "start-time", Value: &startTime},
		{Name: "end-time", Value: &endTime},
		{Name: "stable-hash", Value: &stableHash},
		{Name: "canary-hash", Value: &canaryHash},
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
			// result.judgeResult.score.score
			Body: io.NopCloser(bytes.NewBufferString(`
			{
				"complete" : true,
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

func TestResumeMissingScore(t *testing.T) {
	e := log.Entry{}
	c := NewTestClient(func(req *http.Request) *http.Response {

		assert.Equal(t, req.URL.String(), "https://kayenta.example.oom/canary/01DS50WVHAWSTAQACJKB1VKDQB")

		return &http.Response{
			StatusCode: 200,
			// result.judgeResult.score.score
			Body: io.NopCloser(bytes.NewBufferString(`
			{
				"complete" : true,
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
			// result.judgeResult.score.score
			Body: io.NopCloser(bytes.NewBufferString(`
			{
				"complete" : true,
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
			// result.judgeResult.score.score
			Body: io.NopCloser(bytes.NewBufferString(`
			{
				"complete" : true,
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

func TestResumeIncompleteStatus(t *testing.T) {
	e := log.Entry{}
	c := NewTestClient(func(req *http.Request) *http.Response {

		assert.Equal(t, req.URL.String(), "https://kayenta.example.oom/canary/01DS50WVHAWSTAQACJKB1VKDQB")

		return &http.Response{
			StatusCode: 200,
			// result.judgeResult.score.score
			Body: io.NopCloser(bytes.NewBufferString(`
			{
				"complete" : false,
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
	assert.NotNil(t, measurement.ResumeAt)
	assert.Nil(t, measurement.FinishedAt)
	assert.Equal(t, "01DS50WVHAWSTAQACJKB1VKDQB", measurement.Metadata["canaryExecutionId"])
	assert.Equal(t, v1alpha1.AnalysisPhaseRunning, measurement.Phase)

}

func TestResumeMissingCompleteStatus(t *testing.T) {
	e := log.Entry{}
	c := NewTestClient(func(req *http.Request) *http.Response {

		assert.Equal(t, req.URL.String(), "https://kayenta.example.oom/canary/01DS50WVHAWSTAQACJKB1VKDQB")

		return &http.Response{
			StatusCode: 200,
			// result.judgeResult.score.score
			Body: io.NopCloser(bytes.NewBufferString(`
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
	assert.Equal(t, v1alpha1.AnalysisPhaseError, measurement.Phase)

}

// RoundTripFunc .
type RoundTripFunc func(req *http.Request) *http.Response

// RoundTrip .
func (f RoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req), nil
}

// NewTestClient returns *http.Client with Transport replaced to avoid making real calls
func NewTestClient(fn RoundTripFunc) http.Client {
	return http.Client{
		Transport: fn,
	}
}
