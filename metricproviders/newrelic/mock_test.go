package newrelic

import "github.com/newrelic/newrelic-client-go/pkg/nrdb"

type mockAPI struct {
	response []nrdb.NRDBResult
	err      error
}

func (m *mockAPI) Query(query string) ([]nrdb.NRDBResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.response, nil
}
