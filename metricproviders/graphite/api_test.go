package graphite

import (
	"errors"
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
	query := "target=sumSeries(app.http.*.*.count)&from=-2min"
	targetQuery := "sumSeries(app.http.*.*.count)"
	fromQuery := "-2min"
	goodResult := float64(100)

	tests := []struct {
		name           string
		query          string
		expectedTarget string
		expectedFrom   string
		expectedResult *float64
		expectedErr    error
		body           string
		responseCode   int
	}{{
		"ok",
		query,
		targetQuery,
		fromQuery,
		&goodResult,
		nil,
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
		200,
	}, {
		"graphite response body with invalid JSON",
		query,
		targetQuery,
		fromQuery,
		nil,
		errors.New("invalid character 'i' looking for beginning of value"),
		"invalid JSON",
		200,
	}, {
		"400 graphite response status",
		query,
		targetQuery,
		fromQuery,
		nil,
		errors.New("error response: foo"),
		"foo",
		400,
	}, {
		"500 graphite response status",
		query,
		targetQuery,
		fromQuery,
		nil,
		errors.New("error response: bar"),
		"bar",
		500,
	}, {
		"invalid query",
		"target=#$%^&*(proper$#$%%^(password&from=-2min",
		"#$%^&*(proper$#$%%^(password",
		"-2min",
		nil,
		errors.New("parse \"./render?target=#$%^&*(proper$#$%%^(password&from=-2min\": invalid URL escape \"%^&\""),
		"",
		200,
	}, {
		"graphite response data point JSON with only one item",
		query,
		targetQuery,
		fromQuery,
		nil,
		errors.New("error unmarshaling data point: [10]"),
		`[
			{
				"datapoints": [
					[
						10
					]
				],
				"target": "sumSeries(app.http.*.*.count)",
				"tags": {
					"aggregatedBy": "sum",
					"name": "sumSeries(app.http.*.*.count)"
				}
			}
		]`,
		200,
	}, {
		"graphite response data point JSON with an invalid timestamp",
		query,
		targetQuery,
		fromQuery,
		nil,
		errors.New("strconv.ParseInt: parsing \"f\": invalid syntax"),
		`[
			{
				"datapoints": [
					[
						100,
						"f"
					]
				],
				"target": "sumSeries(app.http.*.*.count)"
			}
		]`,
		200,
	}, {
		"graphite response data point JSON with a string value",
		query,
		targetQuery,
		fromQuery,
		&goodResult,
		nil,
		`[
			{
				"datapoints": [
					[
						"100",
						1621348420
					]
				],
				"target": "sumSeries(app.http.*.*.count)"
			}
		]`,
		200,
	}, {
		"graphite response data point JSON triggers unmarshaling error",
		query,
		targetQuery,
		fromQuery,
		nil,
		errors.New("error unmarshaling value: []"),
		`[
			{
				"datapoints": [
					[
						[],
						1621348420
					]
				],
				"target": "sumSeries(app.http.*.*.count)"
			}
		]`,
		200,
	}, {
		"graphite response data point JSON with a string timestamp",
		query,
		targetQuery,
		fromQuery,
		&goodResult,
		nil,
		`[
			{
				"datapoints": [
					[
						100,
						"1621348420"
					]
				],
				"target": "sumSeries(app.http.*.*.count)"
			}
		]`,
		200,
	}, {
		"graphite response data point timestamp JSON triggers unmarshaling error",
		query,
		targetQuery,
		fromQuery,
		nil,
		errors.New("error unmarshaling timestamp: 100"),
		`[
			{
				"datapoints": [
					[
						100,
						[]
					]
				],
				"target": "sumSeries(app.http.*.*.count)"
			}
		]`,
		200,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				target := r.URL.Query().Get("target")
				assert.Equal(t, test.expectedTarget, target)

				from := r.URL.Query().Get("from")
				assert.Equal(t, test.expectedFrom, from)

				w.WriteHeader(test.responseCode)

				json := test.body
				w.Write([]byte(json))
			}))
			defer ts.Close()

			g, err := NewAPIClient(testGraphiteMetric(ts.URL), log.Entry{})
			assert.Nil(t, err)

			val, err := g.Query(test.query)
			if test.expectedErr != nil {
				assert.Equal(t, err.Error(), test.expectedErr.Error())
			} else {
				assert.Nil(t, err)
			}
			assert.Equal(t, test.expectedResult, val)
		})
	}
}
