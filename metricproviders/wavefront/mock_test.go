package wavefront

import (
	wavefront_api "github.com/spaceapegames/go-wavefront"
)

type mockAPI struct {
	response *wavefront_api.QueryResponse
	err      error
}

type mockQuery struct {
	response *wavefront_api.QueryResponse
	err      error
}

func (m mockAPI) NewQuery(queryParams *wavefront_api.QueryParams) WavefrontQueryAPI {
	return mockQuery{
		response: m.response,
		err:      m.err,
	}
}

func (q mockQuery) Execute() (*wavefront_api.QueryResponse, error) {
	if q.err != nil {
		return nil, q.err
	}
	return q.response, nil
}
