package downloader_test

import (
	"testing"

	"adaptive-downloader/pkg/downloader"
	"adaptive-downloader/pkg/policy"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubPolicy struct{}

func (stubPolicy) SelectAction() policy.Action {
	return policy.Action{
		Concurrency: 2,
		ChunkSize:   1024,
	}
}

func (stubPolicy) Observe(policy.Context) {}

var _ policy.Policy = stubPolicy{}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name        string
		config      downloader.Config
		wantErr     bool
		errContains string
	}{
		{
			name: "valid config",
			config: downloader.Config{
				Policy:      stubPolicy{},
				Concurrency: 2,
				ChunkSize:   1024,
			},
		},
		{
			name: "nil policy",
			config: downloader.Config{
				Concurrency: 2,
				ChunkSize:   1024,
			},
			wantErr:     true,
			errContains: "policy must not be nil",
		},
		{
			name: "zero concurrency",
			config: downloader.Config{
				Policy:      stubPolicy{},
				Concurrency: 0,
				ChunkSize:   1024,
			},
			wantErr:     true,
			errContains: "concurrency must be greater than 0",
		},
		{
			name: "negative concurrency",
			config: downloader.Config{
				Policy:      stubPolicy{},
				Concurrency: -1,
				ChunkSize:   1024,
			},
			wantErr:     true,
			errContains: "concurrency must be greater than 0",
		},
		{
			name: "zero chunk size",
			config: downloader.Config{
				Policy:      stubPolicy{},
				Concurrency: 2,
				ChunkSize:   0,
			},
			wantErr:     true,
			errContains: "chunk size must be greater than 0",
		},
		{
			name: "negative chunk size",
			config: downloader.Config{
				Policy:      stubPolicy{},
				Concurrency: 2,
				ChunkSize:   -1,
			},
			wantErr:     true,
			errContains: "chunk size must be greater than 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestNew(t *testing.T) {
	t.Run("creates downloader with valid config", func(t *testing.T) {
		config := downloader.Config{
			Policy:      stubPolicy{},
			Concurrency: 2,
			ChunkSize:   1024,
		}

		d, err := downloader.New(config)

		require.NoError(t, err)
		require.NotNil(t, d)
	})

	t.Run("rejects invalid config", func(t *testing.T) {
		config := downloader.Config{
			Concurrency: 2,
			ChunkSize:   1024,
		}

		d, err := downloader.New(config)

		require.Error(t, err)
		assert.Nil(t, d)
	})
}
