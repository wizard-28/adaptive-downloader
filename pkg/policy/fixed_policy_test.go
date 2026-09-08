package policy_test

import (
	"testing"

	"adaptive-downloader/pkg/policy"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewFixed(t *testing.T) {
	t.Run("creates fixed policy", func(t *testing.T) {
		action := policy.Action{
			Concurrency: 4,
			ChunkSize:   1024 * 1024,
		}

		p, err := policy.NewFixed(action)

		require.NoError(t, err)
		require.NotNil(t, p)

		assert.Equal(t, action, p.SelectAction())
	})

	t.Run("rejects invalid concurrency", func(t *testing.T) {
		action := policy.Action{
			Concurrency: 0,
			ChunkSize:   1024,
		}

		p, err := policy.NewFixed(action)

		require.Error(t, err)
		assert.Nil(t, p)
	})

	t.Run("rejects invalid chunk size", func(t *testing.T) {
		action := policy.Action{
			Concurrency: 4,
			ChunkSize:   0,
		}

		p, err := policy.NewFixed(action)

		require.Error(t, err)
		assert.Nil(t, p)
	})

	t.Run("rejects negative concurrency", func(t *testing.T) {
		action := policy.Action{
			Concurrency: -1,
			ChunkSize:   1024,
		}

		p, err := policy.NewFixed(action)

		require.Error(t, err)
		assert.Nil(t, p)
	})

	t.Run("rejects negative chunk size", func(t *testing.T) {
		action := policy.Action{
			Concurrency: 4,
			ChunkSize:   -1,
		}

		p, err := policy.NewFixed(action)

		require.Error(t, err)
		assert.Nil(t, p)
	})
}

func TestFixedPolicy_SelectAction(t *testing.T) {
	expected := policy.Action{
		Concurrency: 8,
		ChunkSize:   4 * 1024 * 1024,
	}

	p, err := policy.NewFixed(expected)
	require.NoError(t, err)

	actual := p.SelectAction()

	assert.Equal(t, expected, actual)
}

func TestFixedPolicy_Observe(t *testing.T) {
	action := policy.Action{
		Concurrency: 4,
		ChunkSize:   2 * 1024 * 1024,
	}

	p, err := policy.NewFixed(action)
	require.NoError(t, err)

	before := p.SelectAction()

	p.Observe(policy.Context{
		Throughput:         100,
		PerConnectionRate:  25,
		Latency:            50,
		ErrorRate:          0.1,
		RetryRate:          0.05,
		ActiveConnections:  8,
		CurrentConcurrency: 8,
		CurrentChunkSize:   16 * 1024 * 1024,
	})

	after := p.SelectAction()

	assert.Equal(t, before, after)
	assert.Equal(t, action, after)
}
