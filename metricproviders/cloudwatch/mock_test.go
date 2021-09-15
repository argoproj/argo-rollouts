package cloudwatch

import (
	"time"

	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
)

type mockAPI struct {
	response *cloudwatch.GetMetricDataOutput
	err      error
}

func (m *mockAPI) Query(interval time.Duration, query []types.MetricDataQuery) (*cloudwatch.GetMetricDataOutput, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.response, nil
}
