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

	r := gqlNrglQueryResponse{}
	r.Actor.Account.NRQL.Results = m.response

	rVal := reflect.ValueOf(r)
	reflect.ValueOf(respBody).Elem().Set(rVal)

	return nil
}

// Response sets the response the mock client will return
func (m *mockNerdGraphClient) Response(response []nrdb.NRDBResult) {
	m.response = response
}

// LastArgs returns the variables used when calling the NerdGraph API
func (m *mockNerdGraphClient) LastArgs() map[string]any {
	return m.lastArgs
}

// Err sets the error to be returned when calling the NerdGraph API
func (m *mockNerdGraphClient) Err(err error) {
	m.err = err
}

// Clear removes all configured mock behavior
func (m *mockNerdGraphClient) Clear() {
	m.err = nil
	m.response = nil
	m.lastArgs = nil
}
