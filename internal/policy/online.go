package policy

import (
	"errors"
	"fmt"
	"math"
	"time"
)

// ErrInvalidOnlineConfig indicates an unusable OnlineTunerConfig.
var ErrInvalidOnlineConfig = errors.New("policy: invalid online tuner configuration")

// OnlineTunerConfig configures an OnlineTunerPolicy.
type OnlineTunerConfig struct {
	MinConcurrency int
	MaxConcurrency int

	// Interval is the length of one measurement window. Aggregate throughput
	// is the bytes of all attempts completed in the window over its length.
	Interval time.Duration
	// InitialStep is the first concurrency step; it halves on every reversal
	// and never drops below 1.
	InitialStep int
	// Penalty is the per-connection utility penalty K in U = T / K^N
	// (Falcon-style); 1 means no penalty. Must be >= 1.
	Penalty float64
	// Tolerance is the relative utility drop treated as "worse". 0.05 means a
	// window must be at least 5 % worse than the previous one to reverse.
	Tolerance float64

	// Now returns the current time; nil uses time.Now. Tests inject a clock.
	Now func() time.Time
}

func (c OnlineTunerConfig) validate() error {
	switch {
	case c.MinConcurrency < 1:
		return fmt.Errorf("%w: min concurrency must be >= 1, got %d", ErrInvalidOnlineConfig, c.MinConcurrency)
	case c.MaxConcurrency < c.MinConcurrency:
		return fmt.Errorf("%w: max concurrency (%d) must be >= min (%d)", ErrInvalidOnlineConfig, c.MaxConcurrency, c.MinConcurrency)
	case c.Interval <= 0:
		return fmt.Errorf("%w: interval must be positive, got %v", ErrInvalidOnlineConfig, c.Interval)
	case c.InitialStep < 1:
		return fmt.Errorf("%w: initial step must be >= 1, got %d", ErrInvalidOnlineConfig, c.InitialStep)
	case !(c.Penalty >= 1) || math.IsInf(c.Penalty, 0):
		return fmt.Errorf("%w: penalty must be finite and >= 1, got %v", ErrInvalidOnlineConfig, c.Penalty)
	case !validThreshold(c.Tolerance):
		return fmt.Errorf("%w: tolerance must be finite and >= 0, got %v", ErrInvalidOnlineConfig, c.Tolerance)
	}
	return nil
}

// OnlineTunerPolicy is a deterministic online concurrency tuner in the style
// of FastBioDL and Falcon: it measures aggregate throughput over fixed time
// windows, scores each window with U = T / Penalty^N, keeps moving N in the
// same direction while U does not get worse, and reverses with a halved step
// when it does. It never changes chunk size and ignores reward.
//
// It is a re-implementation of the idea, not of either system's code.
type OnlineTunerPolicy struct {
	cfg OnlineTunerConfig
	now func() time.Time

	started     bool
	windowStart time.Time
	windowBytes int64

	haveUtility bool
	utility     float64
	direction   int
	step        int
}

// NewOnlineTunerPolicy validates cfg and returns a tuner that first moves up.
func NewOnlineTunerPolicy(cfg OnlineTunerConfig) (*OnlineTunerPolicy, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &OnlineTunerPolicy{cfg: cfg, now: now, direction: 1, step: cfg.InitialStep}, nil
}

// SelectAction accumulates the completed attempt's bytes and, once a window
// has elapsed, returns at most one concurrency move. Action.Value carries the
// number of unit steps.
func (p *OnlineTunerPolicy) SelectAction(ctx Context) Action {
	t := p.now()
	if !p.started {
		p.started, p.windowStart = true, t
	}
	p.windowBytes += ctx.BytesTransferred
	elapsed := t.Sub(p.windowStart)
	if elapsed < p.cfg.Interval {
		return NoChange()
	}

	throughput := float64(p.windowBytes) / elapsed.Seconds()
	u := throughput / math.Pow(p.cfg.Penalty, float64(ctx.Concurrency))
	if p.haveUtility && u < p.utility*(1-p.cfg.Tolerance) {
		p.direction = -p.direction
		if p.step > 1 {
			p.step /= 2
		}
	}
	p.haveUtility, p.utility = true, u
	p.windowStart, p.windowBytes = t, 0

	// Clamp at the bounds and keep the direction: at a bound the tuner holds
	// until a worse window turns it around.
	target := min(max(ctx.Concurrency+p.direction*p.step, p.cfg.MinConcurrency), p.cfg.MaxConcurrency)
	switch d := target - ctx.Concurrency; {
	case d > 0:
		return Action{Kind: ActionIncreaseConcurrency, Value: int64(d)}
	case d < 0:
		return Action{Kind: ActionDecreaseConcurrency, Value: int64(-d)}
	}
	return NoChange()
}

// Update is a no-op: the tuner uses no reward.
func (p *OnlineTunerPolicy) Update(Context, Action, float64) {}
