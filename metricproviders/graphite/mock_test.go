package graphite

type mockAPI struct {
	response []dataPoint
	err      error
}

func (m mockAPI) Query(query string) ([]dataPoint, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.response, nil
}
