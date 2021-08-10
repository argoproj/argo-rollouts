package graphite

type mockAPI struct {
	response *float64
	err      error
}

func (m mockAPI) Query(query string) (*float64, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.response, nil
}
