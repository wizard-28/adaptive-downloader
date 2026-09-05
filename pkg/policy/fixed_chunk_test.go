package policy_test

import (
	"testing"

	"adaptive-downloader/pkg/policy"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewFixedChunkPolicy(t *testing.T) {
	t.Run("accepts positive chunk size", func(t *testing.T) {
		p, err := policy.NewFixedChunkPolicy(1024)

		require.NoError(t, err)
		require.NotNil(t, p)
		assert.Equal(t, int64(1024), p.ChunkSize())
	})

	t.Run("rejects zero chunk size", func(t *testing.T) {
		p, err := policy.NewFixedChunkPolicy(0)

		require.Error(t, err)
		assert.Nil(t, p)
	})

	t.Run("rejects negative chunk size", func(t *testing.T) {
		p, err := policy.NewFixedChunkPolicy(-1024)

		require.Error(t, err)
		assert.Nil(t, p)
	})
}

func TestFixedChunkPolicyKeepsChunkSizeConstant(t *testing.T) {
	p, err := policy.NewFixedChunkPolicy(4096)
	require.NoError(t, err)

	for i := 0; i < 100; i++ {
		assert.Equal(t, int64(4096), p.ChunkSize())
	}
}
