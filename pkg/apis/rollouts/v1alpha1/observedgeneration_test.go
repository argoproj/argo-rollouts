package v1alpha1

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests pin the wire contract for status.observedGeneration and
// status.workloadObservedGeneration to an integer, matching the Kubernetes API
// convention. Tooling such as FluxCD's kstatus reads observedGeneration as an
// int64 and compares it against metadata.generation; when the field was a
// string, kstatus could not evaluate the resource at all. If the type ever
// regresses to a string these tests fail fast. See issue #3402.

func TestObservedGenerationSerializesAsInteger(t *testing.T) {
	status := RolloutStatus{
		ObservedGeneration:         7,
		WorkloadObservedGeneration: 3,
	}
	b, err := json.Marshal(status)
	require.NoError(t, err)
	s := string(b)

	assert.Contains(t, s, `"observedGeneration":7`)
	assert.Contains(t, s, `"workloadObservedGeneration":3`)
	// Must never be quoted -- the pre-#3402 behaviour that broke kstatus.
	assert.NotContains(t, s, `"observedGeneration":"7"`)
	assert.NotContains(t, s, `"workloadObservedGeneration":"3"`)
}

func TestObservedGenerationOmitEmpty(t *testing.T) {
	b, err := json.Marshal(RolloutStatus{})
	require.NoError(t, err)
	s := string(b)
	assert.NotContains(t, s, "observedGeneration")
	assert.NotContains(t, s, "workloadObservedGeneration")
}

func TestObservedGenerationProtobufRoundTrip(t *testing.T) {
	orig := RolloutStatus{
		ObservedGeneration:         123456789,
		WorkloadObservedGeneration: 987654321,
	}
	data, err := orig.Marshal()
	require.NoError(t, err)
	require.Equal(t, orig.Size(), len(data))

	var got RolloutStatus
	require.NoError(t, got.Unmarshal(data))
	assert.Equal(t, int64(123456789), got.ObservedGeneration)
	assert.Equal(t, int64(987654321), got.WorkloadObservedGeneration)
}
