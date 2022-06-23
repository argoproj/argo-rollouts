package influxdb

import (
	"context"

	influxapi "github.com/influxdata/influxdb-client-go/v2/api"
	"github.com/influxdata/influxdb-client-go/v2/domain"
)

type mockAPI struct {
	response *influxapi.QueryTableResult
	err      error
}

func (m mockAPI) Query(ctx context.Context, query string) (*influxapi.QueryTableResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.response, nil
}

func (m mockAPI) QueryRaw(context.Context, string, *domain.Dialect) (string, error) {
	panic("Not used")
}

func (m mockAPI) QueryRawWithParams(ctx context.Context, query string, dialect *domain.Dialect, params interface{}) (string, error) {
	panic("Not used")
}

func (m mockAPI) QueryWithParams(ctx context.Context, query string, params interface{}) (*influxapi.QueryTableResult, error) {
	panic("Not used")
}
