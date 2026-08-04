package skywalking

type mockAPI struct {
	err     error
	results any
}

func (m mockAPI) Query(query string) (any, error) {
	if m.err != nil {
		return m.results, m.err
	}
	return m.results, nil
}
