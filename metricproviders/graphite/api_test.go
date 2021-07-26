package graphite

import (
	"fmt"
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
