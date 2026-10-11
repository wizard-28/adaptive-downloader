package policy

import (
	"errors"
	"fmt"
	"math"
)

// ErrInvalidPolicyConfig indicates an unusable ThroughputPolicyConfig.
var ErrInvalidPolicyConfig = errors.New("policy: invalid throughput policy configuration")

// ThroughputPolicyConfig configures a ThroughputPolicy.
type ThroughputPolicyConfig struct {
	MinConcurrency int
	MaxConcurrency int

	// ImprovementThreshold is the minimum relative throughput gain required to
	// increase concurrency. 0.10 means "at least +10%".
	ImprovementThreshold float64
	// DegradationThreshold is the minimum relative throughput loss required to
	// decrease concurrency. 0.10 means "at least -10%".
	DegradationThreshold float64
}

func (c ThroughputPolicyConfig) validate() error {
	switch {
	case c.MinConcurrency < 1:
		return fmt.Errorf("%w: min concurrency must be >= 1, got %d", ErrInvalidPolicyConfig, c.MinConcurrency)
	case c.MaxConcurrency < c.MinConcurrency:
		return fmt.Errorf("%w: max concurrency (%d) must be >= min (%d)", ErrInvalidPolicyConfig, c.MaxConcurrency, c.MinConcurrency)
	case !validThreshold(c.ImprovementThreshold):
		return fmt.Errorf("%w: improvement threshold must be finite and >= 0, got %v", ErrInvalidPolicyConfig, c.ImprovementThreshold)
	case !validThreshold(c.DegradationThreshold):
		return fmt.Errorf("%w: degradation threshold must be finite and >= 0, got %v", ErrInvalidPolicyConfig, c.DegradationThreshold)
	}
	return nil
}

func validThreshold(v float64) bool {
	return v >= 0 && !math.IsNaN(v) && !math.IsInf(v, 0)
}

// ThroughputPolicy is a rule-based, non-learning policy that scales concurrency
// from the relative change in throughput between consecutive observations.
//
// It never changes chunk size, never uses reward, and applies no smoothing: the
// comparison point is always the immediately previous observation. Relative
// thresholds keep it from reacting to small fluctuations.
type ThroughputPolicy struct {
	minConcurrency int
	maxConcurrency int
	improve        float64
	degrade        float64

	havePrevious bool
	previous     float64
}

// NewThroughputPolicy validates cfg and returns a policy with no baseline yet.
func NewThroughputPolicy(cfg ThroughputPolicyConfig) (*ThroughputPolicy, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &ThroughputPolicy{
		minConcurrency: cfg.MinConcurrency,
		maxConcurrency: cfg.MaxConcurrency,
		improve:        cfg.ImprovementThreshold,
		degrade:        cfg.DegradationThreshold,
	}, nil
}

// SelectAction compares ctx.Throughput with the previous observation and
// returns a concurrency action. The first call records the baseline and returns
// NoChange. The previous throughput is updated on every call, including calls
// that return NoChange.
//
// Decision rule (relative change = (current-previous)/previous):
//
//	change >= +improvementThreshold  -> increase (unless at max)
//	change <= -degradationThreshold  -> decrease (unless at min)
//	otherwise                        -> no change
//
// Zero-throughput cases are defined explicitly: 0 -> 0 is no change; a rise
// from 0 is treated as improvement; a fall to 0 is treated as degradation.
func (p *ThroughputPolicy) SelectAction(ctx Context) Action {
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
		// Recovered from a zero/unmeasured throughput: treat as improvement.
		if ctx.Concurrency < p.maxConcurrency {
			return IncreaseConcurrency()
		}
		return NoChange()
	case current <= 0:
		// Throughput collapsed to zero: treat as degradation.
		if ctx.Concurrency > p.minConcurrency {
			return DecreaseConcurrency()
		}
		return NoChange()
	}

	change := (current - previous) / previous
	switch {
	case change > 0 && change >= p.improve:
		if ctx.Concurrency < p.maxConcurrency {
			return IncreaseConcurrency()
		}
	case change < 0 && change <= -p.degrade:
		if ctx.Concurrency > p.minConcurrency {
			return DecreaseConcurrency()
		}
	}
	return NoChange()
}

// Update intentionally ignores reward: this rule-based policy learns nothing.
// All state needed for the next decision is maintained by SelectAction, so
// Update has no effect on future actions.
func (p *ThroughputPolicy) Update(Context, Action, float64) {}

var _ Policy = (*ThroughputPolicy)(nil)
