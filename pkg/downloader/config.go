package downloader

import (
	"fmt"

	"adaptive-downloader/pkg/policy"
)

const (
	DefaultConcurrency = 2
	DefaultChunkSize   = 5 * 1024 * 1024
)

// Config contains the configuration required to perform a download.
//
// Policy is supplied by the caller so the downloader does not need to know
// whether the selected policy is fixed, bandit-based, RL-based, or remote.
type Config struct {
	OutputPath string
	Policy     policy.Policy

	Concurrency int
	ChunkSize   int64

	SHA256     string
	NoProgress bool
}

// Validate checks the configuration required by the current fixed-worker
// execution path.
func (c Config) Validate() error {
	if c.Policy == nil {
		return fmt.Errorf("policy must not be nil")
	}

	if c.Concurrency <= 0 {
		return fmt.Errorf(
			"concurrency must be greater than 0, got %d",
			c.Concurrency,
		)
	}

	if c.ChunkSize <= 0 {
		return fmt.Errorf(
			"chunk size must be greater than 0, got %d",
			c.ChunkSize,
		)
	}

	return nil
}
