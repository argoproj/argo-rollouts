package apisix

import (
	"testing"

	"github.com/tj/assert"

	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/apisix/mocks"
)

func TestNewDynamicClient(t *testing.T) {
	t.Run("NewDynamicClient", func(t *testing.T) {
		// Given
		t.Parallel()
		fakeDynamicClient := &mocks.FakeDynamicClient{}

		// When
		NewDynamicClient(fakeDynamicClient, "default")
	})
}

func TestDoesApisixExist(t *testing.T) {
	t.Run("exist", func(t *testing.T) {
		fakeDynamicClient := &mocks.FakeDynamicClient{}
		assert.True(t, DoesApisixExist(fakeDynamicClient, ""))
	})
	t.Run("not exist", func(t *testing.T) {
		fakeDynamicClient := &mocks.FakeDynamicClient{
			IsListError: true,
		}
		assert.False(t, DoesApisixExist(fakeDynamicClient, ""))
	})
}
