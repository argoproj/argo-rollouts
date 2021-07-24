package graphite

import (
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

func TestType_withValidURL(t *testing.T) {
	e := log.Entry{}
	m := v1alpha1.Metric{
		Provider: v1alpha1.MetricProvider{
			Graphite: &v1alpha1.GraphiteMetric{
				Address: "http://some-graphite.foo",
			},
		},
	}
	g, err := NewGraphiteProvider(m, e)

	assert.Nil(t, err)
	assert.Equal(t, ProviderType, g.Type())
}
