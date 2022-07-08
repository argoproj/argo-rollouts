package prometheus

import (
	"context"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

type mockAPI struct {
	value    model.Value
	err      error
	warnings v1.Warnings
}

func (m mockAPI) WalReplay(ctx context.Context) (v1.WalReplayStatus, error) {
	panic("Not used")
}

// Query performs a query for the given time.
func (m mockAPI) Query(ctx context.Context, query string, ts time.Time, opt ...v1.Option) (model.Value, v1.Warnings, error) {
	if m.err != nil {
		return nil, m.warnings, m.err
	}
	return m.value, m.warnings, nil
}

// Below methods are not used but required for the interface implementation

func (m mockAPI) Metadata(ctx context.Context, metric string, limit string) (map[string][]v1.Metadata, error) {
	panic("Not used")
}

func (m mockAPI) CleanTombstones(ctx context.Context) error {
	panic("Not used")
}

func (m mockAPI) DeleteSeries(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) error {
	panic("Not used")
}

func (m mockAPI) LabelNames(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) ([]string, v1.Warnings, error) {
	panic("Not used")
}

func (m mockAPI) LabelValues(ctx context.Context, label string, matches []string, startTime time.Time, endTime time.Time) (model.LabelValues, v1.Warnings, error) {
	panic("Not used")
}

func (m mockAPI) QueryRange(ctx context.Context, query string, r v1.Range, opt ...v1.Option) (model.Value, v1.Warnings, error) {
	panic("Not used")
}

func (m mockAPI) Series(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) ([]model.LabelSet, v1.Warnings, error) {
	panic("Not used")
}

func (m mockAPI) Targets(ctx context.Context) (v1.TargetsResult, error) {
	panic("Not used")
}

func (m mockAPI) Alerts(ctx context.Context) (v1.AlertsResult, error) {
	panic("Not used")
}

func (m mockAPI) AlertManagers(ctx context.Context) (v1.AlertManagersResult, error) {
	panic("Not used")
}

func (m mockAPI) Config(ctx context.Context) (v1.ConfigResult, error) {
	panic("Not used")
}

func (m mockAPI) Flags(ctx context.Context) (v1.FlagsResult, error) {
	panic("Not used")
}

func (m mockAPI) Snapshot(ctx context.Context, skipHead bool) (v1.SnapshotResult, error) {
	panic("Not used")
}

func (m mockAPI) Rules(ctx context.Context) (v1.RulesResult, error) {
	panic("Not used")
}

func (m mockAPI) TargetsMetadata(ctx context.Context, matchTarget string, metric string, limit string) ([]v1.MetricMetadata, error) {
	panic("Not used")
}

func (m mockAPI) Runtimeinfo(ctx context.Context) (v1.RuntimeinfoResult, error) {
	panic("Not used")
}

func (m mockAPI) TSDB(ctx context.Context) (v1.TSDBResult, error) {
	panic("Not used")
}

func (m mockAPI) Buildinfo(ctx context.Context) (v1.BuildinfoResult, error) {
	panic("Not used")
}

func (m mockAPI) QueryExemplars(ctx context.Context, query string, startTime time.Time, endTime time.Time) ([]v1.ExemplarQueryResult, error) {
	panic("Not used")
}
