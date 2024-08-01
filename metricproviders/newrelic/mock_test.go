package newrelic

import (
	"reflect"

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

type mockNerdGraphClient struct {
	response []nrdb.NRDBResult
	lastArgs map[string]interface{}
	err      error
}

func (m *mockNerdGraphClient) QueryWithResponse(query string, variables map[string]interface{}, respBody interface{}) error {
	m.lastArgs = variables

	if m.err != nil {
		return m.err
	}

	r := gqlNrglQueryResponse{
		Actor{Account{NRQL: nrdb.NRDBResultContainer{
			Results: m.response,
		}}},
	}
	rVal := reflect.ValueOf(r)
	reflect.ValueOf(respBody).Elem().Set(rVal)

	return nil
}

func (m *mockNerdGraphClient) Response(response []nrdb.NRDBResult) {
	m.response = response
}

func (m *mockNerdGraphClient) LastArgs() map[string]any {
	return m.lastArgs
}

func (m *mockNerdGraphClient) Err(err error) {
	m.err = err
}

func (m *mockNerdGraphClient) Clear() {
	m.err = nil
	m.response = nil
	m.lastArgs = nil
}
