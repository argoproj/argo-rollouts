package webmetric

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestRunSuccess(t *testing.T) {
	input := `
	{
		"key": [
			{
				"key2": {
					"value": 1
				}
			}
		]
	}`

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.Header().Set("Content-Type", "application/json")
		io.WriteString(rw, input)
	}))
	// Close the server when test finishes
	defer server.Close()

	e := log.Entry{}

	metric := v1alpha1.Metric{
		Name:             "foo",
		SuccessCondition: "result > 0",
		FailureCondition: "result <= 0",
		Provider: v1alpha1.MetricProvider{
			Web: &v1alpha1.WebMetric{
				URL:      server.URL,
				JSONPath: "{$.key[0].key2.value}",
			},
		},
	}

	jp, err := NewWebMetricJsonParser(metric)
	assert.NoError(t, err)

	p := NewWebMetricProvider(e, server.Client(), jp)

	measurement := p.Run(newAnalysisRun(), metric)
	assert.NotNil(t, measurement.StartedAt)
	assert.Equal(t, "1", measurement.Value)
	assert.NotNil(t, measurement.FinishedAt)
	assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, measurement.Phase)
}

func newAnalysisRun() *v1alpha1.AnalysisRun {
	return &v1alpha1.AnalysisRun{}
}
