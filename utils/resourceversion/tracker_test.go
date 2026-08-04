package resourceversion

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCompareResourceVersion(t *testing.T) {
	tests := []struct {
		name    string
		a       string
		b       string
		want    int
		wantErr bool
	}{
		{name: "equal", a: "100", b: "100", want: 0},
		{name: "less by length", a: "9", b: "10", want: -1},
		{name: "greater by length", a: "100", b: "99", want: 1},
		{name: "less same length", a: "199", b: "200", want: -1},
		{name: "empty a", a: "", b: "1", wantErr: true},
		{name: "malformed", a: "abc", b: "100", wantErr: true},
		{name: "leading zero", a: "01", b: "1", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CompareResourceVersion(tt.a, tt.b)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestTrackerIsCacheStale(t *testing.T) {
	keyA := "default/a"
	keyB := "default/b"

	tests := []struct {
		name      string
		recorded  map[string]string
		queryKey  string
		cachedRV  string
		wantStale bool
	}{
		{name: "empty tracker", queryKey: keyA, cachedRV: "100"},
		{name: "cache caught up", recorded: map[string]string{keyA: "100"}, queryKey: keyA, cachedRV: "100"},
		{name: "external update after us", recorded: map[string]string{keyA: "100"}, queryKey: keyA, cachedRV: "101"},
		{name: "cache behind our write", recorded: map[string]string{keyA: "200"}, queryKey: keyA, cachedRV: "199", wantStale: true},
		{name: "per-key isolation", recorded: map[string]string{keyA: "999"}, queryKey: keyB, cachedRV: "1"},
		{name: "empty cached RV", recorded: map[string]string{keyA: "200"}, queryKey: keyA, cachedRV: ""},
		{name: "malformed stored RV fails open", recorded: map[string]string{keyA: "abc"}, queryKey: keyA, cachedRV: "100"},
		{name: "malformed cached RV fails open", recorded: map[string]string{keyA: "100"}, queryKey: keyA, cachedRV: "abc"},
		{name: "nine vs ten is stale", recorded: map[string]string{keyA: "10"}, queryKey: keyA, cachedRV: "9", wantStale: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracker := NewTracker()
			for k, v := range tt.recorded {
				tracker.Record(k, v)
			}
			assert.Equal(t, tt.wantStale, tracker.IsCacheStale(tt.queryKey, tt.cachedRV))
		})
	}
}

func TestTrackerRecord(t *testing.T) {
	key := "default/a"
	tracker := NewTracker()

	tracker.Record(key, "500")
	tracker.Record(key, "")
	assert.True(t, tracker.IsCacheStale(key, "499"))

	tracker.Record(key, "501")
	assert.True(t, tracker.IsCacheStale(key, "500"))
	assert.False(t, tracker.IsCacheStale(key, "501"))
}

func TestTrackerForget(t *testing.T) {
	keyA := "default/a"
	keyB := "default/b"
	tracker := NewTracker()

	tracker.Record(keyA, "500")
	tracker.Record(keyB, "700")
	tracker.Forget(keyA)

	assert.False(t, tracker.IsCacheStale(keyA, "499"))
	assert.True(t, tracker.IsCacheStale(keyB, "699"))

	assert.NotPanics(t, func() { tracker.Forget(keyA) })
}

func TestTrackerConcurrentRecordAndIsCacheStale(t *testing.T) {
	keyA := "default/a"
	keyB := "default/b"
	tracker := NewTracker()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := keyA
			if i%2 == 0 {
				key = keyB
			}
			tracker.Record(key, "1000")
			_ = tracker.IsCacheStale(key, "999")
		}(i)
	}
	wg.Wait()

	assert.True(t, tracker.IsCacheStale(keyA, "999"))
	assert.True(t, tracker.IsCacheStale(keyB, "999"))
}
