package newrelic

import (
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/newrelic/newrelic-client-go/v2/pkg/nrdb"
)

type mockAPI struct {
	response []nrdb.NRDBResult
	err      error
}

func (m *mockAPI) Query(metric v1alpha1.Metric) ([]nrdb.NRDBResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.response, nil
}
