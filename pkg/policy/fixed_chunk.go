package policy

import "errors"

// FixedChunkPolicy keeps the chunk size S constant throughout a download.
type FixedChunkPolicy struct {
	chunkSize int64
}

// NewFixedChunkPolicy creates a fixed-S policy.
func NewFixedChunkPolicy(chunkSize int64) (*FixedChunkPolicy, error) {
	if chunkSize <= 0 {
		return nil, errors.New("chunk size must be positive")
	}

	return &FixedChunkPolicy{
		chunkSize: chunkSize,
	}, nil
}

// ChunkSize returns the fixed chunk size S.
func (p *FixedChunkPolicy) ChunkSize() int64 {
	return p.chunkSize
}
