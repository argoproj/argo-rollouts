package graphite

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func testGraphiteMetric(addr string) v1alpha1.Metric {
	return v1alpha1.Metric{
		Provider: v1alpha1.MetricProvider{
			Graphite: &v1alpha1.GraphiteMetric{
				Address: addr,
			},
		},
	}
}

func TestNewAPIClientWithValidURL(t *testing.T) {
	e := log.Entry{}
	_, err := NewAPIClient(testGraphiteMetric("http://some-graphite.foo"), e)

	assert.NoError(t, err)
}

func TestNewAPIWithInvalidURL(t *testing.T) {
	addr := ":::"
	e := log.Entry{}
	g, err := NewAPIClient(testGraphiteMetric(addr), e)

	assert.Equal(t, err.Error(), fmt.Sprintf("Graphite address %s is not a valid URL", addr))
	assert.Nil(t, g)
}

func TestQuery(t *testing.T) {
	tests := []struct {
		name           string
		query          string
		expectedTarget string
		expectedFrom   string
		expectedResult float64
		body           string
	}{{
		"ok",
		"target=sumSeries(app.http.*.*.count)&from=-2min",
		"sumSeries(app.http.*.*.count)",
		"-2min",
		float64(100),
		`[
			{
				"datapoints": [
					[
						10,
						1621348400
					],
					[
						75,
						1621348410
					],
					[
						25,
						1621348420
					],
					[
						100,
						1621348430
					]
				],
				"target": "sumSeries(app.http.*.*.count)",
				"tags": {
					"aggregatedBy": "sum",
					"name": "sumSeries(app.http.*.*.count)"
				}
			}
		]`,
	}}

	for _, test := range tests {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			target := r.URL.Query().Get("target")
			assert.Equal(t, test.expectedTarget, target)

			from := r.URL.Query().Get("from")
			assert.Equal(t, test.expectedFrom, from)

			json := test.body
			w.Write([]byte(json))
		}))
		defer ts.Close()

		g, err := NewAPIClient(testGraphiteMetric(ts.URL), log.Entry{})
		assert.Nil(t, err)

		val, err := g.Query(test.query)
		assert.Nil(t, err)
		assert.Equal(t, &test.expectedResult, val)
	}
}
