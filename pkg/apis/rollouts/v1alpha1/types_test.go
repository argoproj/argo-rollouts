package v1alpha1

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRolloutPauseDuration(t *testing.T) {
	rp := RolloutPause{}
	assert.Equal(t, int32(0), rp.DurationSeconds())
	rp.Duration = DurationFromInt(10)
	assert.Equal(t, int32(10), rp.DurationSeconds())
	rp.Duration = DurationFromString("10")
	assert.Equal(t, int32(10), rp.DurationSeconds())
	rp.Duration = DurationFromString("10s")
	assert.Equal(t, int32(10), rp.DurationSeconds())
	rp.Duration = DurationFromString("1h")
	assert.Equal(t, int32(3600), rp.DurationSeconds())
	rp.Duration = DurationFromString("1ms")
	assert.Equal(t, int32(0), rp.DurationSeconds())
	rp.Duration = DurationFromString("1z")
	assert.Equal(t, int32(-1), rp.DurationSeconds())
	rp.Duration = DurationFromString("20000000000") // out of int32
	assert.Equal(t, int32(-1), rp.DurationSeconds())
}
