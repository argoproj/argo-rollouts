package prometheus

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

type mockAPI struct {
	value    model.Value
	warnings []string
	err      error
}

// Query performs a query for the given time.
func (m mockAPI) Query(ctx context.Context, query string, ts time.Time) (model.Value, api.Warnings, error) {
	if m.err != nil {
		return nil, nil, m.err
	}
	return m.value, m.warnings, nil
}

// Below methods are not used but required for the interface implementation

func (m mockAPI) Alerts(ctx context.Context) (v1.AlertsResult, error) {
	return v1.AlertsResult{}, nil
}

func (m mockAPI) AlertManagers(ctx context.Context) (v1.AlertManagersResult, error) {
	return v1.AlertManagersResult{}, nil
}

// CleanTombstones removes the deleted data from disk and cleans up the existing tombstones.
func (m mockAPI) CleanTombstones(ctx context.Context) error {
	return nil
}

// Config returns the current Prometheus configuration.
func (m mockAPI) Config(ctx context.Context) (v1.ConfigResult, error) {
	return v1.ConfigResult{}, nil
}

// DeleteSeries deletes data for a selection of series in a time range.
func (m mockAPI) DeleteSeries(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) error {
	return nil
}

// Flags returns the flag values that Prometheus was launched with.
func (m mockAPI) Flags(ctx context.Context) (v1.FlagsResult, error) {
	return v1.FlagsResult{}, nil
}

func (m mockAPI) LabelNames(ctx context.Context) ([]string, api.Warnings, error) {
	return nil, nil, nil
}

// LabelValues performs a query for the values of the given label.
func (m mockAPI) LabelValues(ctx context.Context, label string) (model.LabelValues, api.Warnings, error) {
	return nil, nil, nil
}

// QueryRange performs a query for the given range.
func (m mockAPI) QueryRange(ctx context.Context, query string, r v1.Range) (model.Value, api.Warnings, error) {
	return nil, nil, nil
}

func (m mockAPI) Rules(ctx context.Context) (v1.RulesResult, error) {
	return v1.RulesResult{}, nil
}

// Series finds series by label matchers.
func (m mockAPI) Series(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) ([]model.LabelSet, api.Warnings, error) {
	return nil, nil, nil
}

// Snapshot creates a snapshot of all current data into snapshots/<datetime>-<rand>
// under the TSDB's data directory and returns the directory as response.
func (m mockAPI) Snapshot(ctx context.Context, skipHead bool) (v1.SnapshotResult, error) {
	return v1.SnapshotResult{}, nil
}

// Targets returns an overview of the current state of the Prometheus target discovery.
func (m mockAPI) Targets(ctx context.Context) (v1.TargetsResult, error) {
	return v1.TargetsResult{}, nil
}

func (m mockAPI) TargetsMetadata(ctx context.Context, matchTarget string, metric string, limit string) ([]v1.MetricMetadata, error) {
	return nil, nil
}
