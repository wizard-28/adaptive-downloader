package policy

import (
	"errors"
	"fmt"
)

// ErrInvalidJointPolicyConfig indicates an unusable JointPolicyConfig.
var ErrInvalidJointPolicyConfig = errors.New("policy: invalid joint policy configuration")

// JointPolicyConfig configures a JointPolicy.
type JointPolicyConfig struct {
	MinConcurrency int
	MaxConcurrency int

	MinChunkSize int64
	MaxChunkSize int64

	// ImprovementThreshold / DegradationThreshold are relative throughput
	// thresholds, consistent with ThroughputPolicy.
	ImprovementThreshold float64
	DegradationThreshold float64
}

func (c JointPolicyConfig) validate() error {
	switch {
	case c.MinConcurrency < 1:
		return fmt.Errorf("%w: min concurrency must be >= 1, got %d", ErrInvalidJointPolicyConfig, c.MinConcurrency)
	case c.MaxConcurrency < c.MinConcurrency:
		return fmt.Errorf("%w: max concurrency (%d) must be >= min (%d)", ErrInvalidJointPolicyConfig, c.MaxConcurrency, c.MinConcurrency)
	case c.MinChunkSize <= 0:
		return fmt.Errorf("%w: min chunk size must be > 0, got %d", ErrInvalidJointPolicyConfig, c.MinChunkSize)
	case c.MaxChunkSize < c.MinChunkSize:
		return fmt.Errorf("%w: max chunk size (%d) must be >= min (%d)", ErrInvalidJointPolicyConfig, c.MaxChunkSize, c.MinChunkSize)
	case !validThreshold(c.ImprovementThreshold):
		return fmt.Errorf("%w: improvement threshold must be finite and >= 0, got %v", ErrInvalidJointPolicyConfig, c.ImprovementThreshold)
	case !validThreshold(c.DegradationThreshold):
		return fmt.Errorf("%w: degradation threshold must be finite and >= 0, got %v", ErrInvalidJointPolicyConfig, c.DegradationThreshold)
	}
	return nil
}

// JointPolicy is a deterministic, non-learning policy that can control both
// concurrency and chunk size from a single decision path.
//
// It uses the same relative throughput-change rule as ThroughputPolicy and
// alternates the preferred dimension on each actionable decision: the first
// actionable decision targets concurrency, the second chunk size, the third
// concurrency, and so on. When the preferred dimension is at a bound it falls
// back to the other dimension; if both are at a bound it returns NoChange.
//
// Context.Concurrency and Context.ChunkSize are authoritative; the policy keeps
// no second copy of either. It only returns generic Action values and never
// touches a controller directly.
type JointPolicy struct {
	minConcurrency int
	maxConcurrency int
	minChunkSize   int64
	maxChunkSize   int64
	improve        float64
	degrade        float64

	havePrevious bool
	previous     float64
	// preferConcurrency is the dimension for the next actionable decision.
	preferConcurrency bool
}

// NewJointPolicy validates cfg and returns a policy with no baseline yet.
func NewJointPolicy(cfg JointPolicyConfig) (*JointPolicy, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &JointPolicy{
		minConcurrency:    cfg.MinConcurrency,
		maxConcurrency:    cfg.MaxConcurrency,
		minChunkSize:      cfg.MinChunkSize,
		maxChunkSize:      cfg.MaxChunkSize,
		improve:           cfg.ImprovementThreshold,
		degrade:           cfg.DegradationThreshold,
		preferConcurrency: true,
	}, nil
}

// SelectAction records the throughput baseline on the first call and otherwise
// returns a concurrency or chunk-size action according to the relative change.
func (p *JointPolicy) SelectAction(ctx Context) Action {
	current := ctx.Throughput

	if !p.havePrevious {
		p.previous = current
		p.havePrevious = true
		return NoChange()
	}

	previous := p.previous
	p.previous = current

	var (
		increase   bool
		actionable bool
	)
	switch {
	case previous <= 0 && current <= 0:
		return NoChange()
	case previous <= 0:
		increase, actionable = true, true
	case current <= 0:
		increase, actionable = false, true
	default:
		change := (current - previous) / previous
		switch {
		case change > 0 && change >= p.improve:
			increase, actionable = true, true
		case change < 0 && change <= -p.degrade:
			increase, actionable = false, true
		}
	}
	if !actionable {
		return NoChange()
	}

	action := p.choose(ctx, increase)
	if !action.IsNoChange() {
		p.preferConcurrency = !p.preferConcurrency
	}
	return action
}

// choose returns the action for an improvement/degradation, preferring the
// current dimension and falling back to the other when the preferred one is at
// a bound. It returns NoChange when both dimensions are at a bound.
func (p *JointPolicy) choose(ctx Context, increase bool) Action {
	if increase {
		if p.preferConcurrency {
			if ctx.Concurrency < p.maxConcurrency {
				return IncreaseConcurrency()
			}
			if ctx.ChunkSize < p.maxChunkSize {
				return IncreaseChunkSize()
			}
			return NoChange()
		}
		if ctx.ChunkSize < p.maxChunkSize {
			return IncreaseChunkSize()
		}
		if ctx.Concurrency < p.maxConcurrency {
			return IncreaseConcurrency()
		}
		return NoChange()
	}

	if p.preferConcurrency {
		if ctx.Concurrency > p.minConcurrency {
			return DecreaseConcurrency()
		}
		if ctx.ChunkSize > p.minChunkSize {
			return DecreaseChunkSize()
		}
		return NoChange()
	}
	if ctx.ChunkSize > p.minChunkSize {
		return DecreaseChunkSize()
	}
	if ctx.Concurrency > p.minConcurrency {
		return DecreaseConcurrency()
	}
	return NoChange()
}

// Update intentionally ignores reward: this rule-based policy learns nothing.
// All state needed for the next decision is maintained by SelectAction.
func (p *JointPolicy) Update(Context, Action, float64) {}

var _ Policy = (*JointPolicy)(nil)
