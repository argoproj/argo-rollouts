package skywalking

type mockAPI struct {
	err     error
	results interface{}
}

func (m mockAPI) Query(query string) (interface{}, error) {
	if m.err != nil {
		return m.results, m.err
	}
	return m.results, nil
}
