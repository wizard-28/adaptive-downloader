package policy

import (
	"errors"
	"fmt"
)

// ErrInvalidChunkSizePolicyConfig indicates an unusable ChunkSizePolicyConfig.
var ErrInvalidChunkSizePolicyConfig = errors.New("policy: invalid chunk-size policy configuration")

// ChunkSizePolicyConfig configures a ChunkSizePolicy.
type ChunkSizePolicyConfig struct {
	MinChunkSize int64
	MaxChunkSize int64

	// ImprovementThreshold is the minimum relative throughput gain required to
	// increase the chunk size. 0.10 means "at least +10%".
	ImprovementThreshold float64
	// DegradationThreshold is the minimum relative throughput loss required to
	// decrease the chunk size. 0.10 means "at least -10%".
	DegradationThreshold float64
}

func (c ChunkSizePolicyConfig) validate() error {
	switch {
	case c.MinChunkSize < 1:
		return fmt.Errorf("%w: min chunk size must be >= 1, got %d", ErrInvalidChunkSizePolicyConfig, c.MinChunkSize)
	case c.MaxChunkSize < c.MinChunkSize:
		return fmt.Errorf("%w: max chunk size (%d) must be >= min (%d)", ErrInvalidChunkSizePolicyConfig, c.MaxChunkSize, c.MinChunkSize)
	case !validThreshold(c.ImprovementThreshold):
		return fmt.Errorf("%w: improvement threshold must be finite and >= 0, got %v", ErrInvalidChunkSizePolicyConfig, c.ImprovementThreshold)
	case !validThreshold(c.DegradationThreshold):
		return fmt.Errorf("%w: degradation threshold must be finite and >= 0, got %v", ErrInvalidChunkSizePolicyConfig, c.DegradationThreshold)
	}
	return nil
}

// ChunkSizePolicy is a rule-based, non-learning policy that scales the target
// chunk size from the relative change in throughput between consecutive
// observations.
//
// It never changes concurrency, never uses reward, and applies no smoothing:
// the comparison point is always the immediately previous observation.
// Context.ChunkSize is the authoritative current target; the policy keeps no
// second copy of it.
type ChunkSizePolicy struct {
	minChunk int64
	maxChunk int64
	improve  float64
	degrade  float64

	havePrevious bool
	previous     float64
}

// NewChunkSizePolicy validates cfg and returns a policy with no baseline yet.
func NewChunkSizePolicy(cfg ChunkSizePolicyConfig) (*ChunkSizePolicy, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &ChunkSizePolicy{
		minChunk: cfg.MinChunkSize,
		maxChunk: cfg.MaxChunkSize,
		improve:  cfg.ImprovementThreshold,
		degrade:  cfg.DegradationThreshold,
	}, nil
}

// SelectAction compares ctx.Throughput with the previous observation and
// returns a chunk-size action. The first call records the baseline and returns
// NoChange. The previous throughput is updated on every call.
//
// Decision rule (relative change = (current-previous)/previous):
//
//	change >= +improvementThreshold  -> increase chunk size (unless at max)
//	change <= -degradationThreshold  -> decrease chunk size (unless at min)
//	otherwise                        -> no change
//
// Zero-throughput cases mirror ThroughputPolicy: 0 -> 0 is no change; a rise
// from 0 is improvement; a fall to 0 is degradation.
func (p *ChunkSizePolicy) SelectAction(ctx Context) Action {
	current := ctx.Throughput

	if !p.havePrevious {
		p.previous = current
		p.havePrevious = true
		return NoChange()
	}

	previous := p.previous
	p.previous = current

	switch {
	case previous <= 0 && current <= 0:
		return NoChange()
	case previous <= 0:
		if ctx.ChunkSize < p.maxChunk {
			return IncreaseChunkSize()
		}
		return NoChange()
	case current <= 0:
		if ctx.ChunkSize > p.minChunk {
			return DecreaseChunkSize()
		}
		return NoChange()
	}

	change := (current - previous) / previous
	switch {
	case change > 0 && change >= p.improve:
		if ctx.ChunkSize < p.maxChunk {
			return IncreaseChunkSize()
		}
	case change < 0 && change <= -p.degrade:
		if ctx.ChunkSize > p.minChunk {
			return DecreaseChunkSize()
		}
	}
	return NoChange()
}

// Update intentionally ignores reward: this rule-based policy learns nothing.
// All state needed for the next decision is maintained by SelectAction.
func (p *ChunkSizePolicy) Update(Context, Action, float64) {}

var _ Policy = (*ChunkSizePolicy)(nil)
