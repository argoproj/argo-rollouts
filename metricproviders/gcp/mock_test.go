package gcp

import (
	"context"
	"time"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

type mockAPI struct {
	promResponse       [][]float64
	timeSeriesResponse [][]float64
	metadata           map[string]string
	err                error
	block              bool

	lastProject  string
	lastQuery    string
	lastFilter   string
	lastInterval time.Duration
}

func (m *mockAPI) QueryPromQL(ctx context.Context, project, query string, interval time.Duration) ([][]float64, map[string]string, error) {
	m.lastProject = project
	m.lastQuery = query
	m.lastInterval = interval
	if m.block {
		<-ctx.Done()
		return nil, nil, ctx.Err()
	}
	if m.err != nil {
		return nil, nil, m.err
	}
	return m.promResponse, m.metadata, nil
}

func (m *mockAPI) QueryTimeSeries(ctx context.Context, project string, q *v1alpha1.GCPMetric, interval time.Duration) ([][]float64, map[string]string, error) {
	m.lastProject = project
	m.lastFilter = q.Filter
	m.lastInterval = interval
	if m.block {
		<-ctx.Done()
		return nil, nil, ctx.Err()
	}
	if m.err != nil {
		return nil, nil, m.err
	}
	return m.timeSeriesResponse, m.metadata, nil
}
