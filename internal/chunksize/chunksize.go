// Package chunksize provides a policy-free controller for the desired size of
// FUTURE range allocations.
//
// It represents only the target size. It never resizes ranges that are already
// allocated, in flight, or completed: the downloader reads Current() when it
// asks RangeTracker for the next range, and any range already handed out keeps
// its original boundaries. Deciding when to change the target belongs to a
// future policy layer.
package chunksize

import (
	"errors"
	"fmt"
	"sync"
)

// ErrInvalidConfig indicates an unusable [min, max, initial, step] configuration.
var ErrInvalidConfig = errors.New("chunksize: invalid configuration")

// Controller tracks a target chunk size bounded by [min, max].
//
// It is safe for concurrent use.
type Controller struct {
	mu      sync.Mutex
	current int64
	min     int64
	max     int64
	step    int64
}

// New returns a Controller whose target starts at initial. It returns
// ErrInvalidConfig unless min > 0, max >= min, initial is within [min, max],
// and step > 0.
func New(min, max, initial, step int64) (*Controller, error) {
	switch {
	case min <= 0:
		return nil, fmt.Errorf("%w: min must be > 0, got %d", ErrInvalidConfig, min)
	case max < min:
		return nil, fmt.Errorf("%w: max must be >= min (%d), got %d", ErrInvalidConfig, min, max)
	case initial < min || initial > max:
		return nil, fmt.Errorf("%w: initial %d must be within [%d, %d]", ErrInvalidConfig, initial, min, max)
	case step <= 0:
		return nil, fmt.Errorf("%w: step must be > 0, got %d", ErrInvalidConfig, step)
	}
	return &Controller{current: initial, min: min, max: max, step: step}, nil
}

// Current returns the current target chunk size.
func (c *Controller) Current() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.current
}

// Min returns the configured minimum chunk size.
func (c *Controller) Min() int64 { return c.min }

// Max returns the configured maximum chunk size.
func (c *Controller) Max() int64 { return c.max }

// Step returns the configured increase/decrease step.
func (c *Controller) Step() int64 { return c.step }

// Increase raises the target by step, up to max, and returns the new target.
func (c *Controller) Increase() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.current = c.clamp(c.current + c.step)
	return c.current
}

// Decrease lowers the target by step, down to min, and returns the new target.
func (c *Controller) Decrease() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.current = c.clamp(c.current - c.step)
	return c.current
}

// Set clamps target to [min, max], stores it, and returns the stored value.
func (c *Controller) Set(target int64) int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.current = c.clamp(target)
	return c.current
}

func (c *Controller) clamp(v int64) int64 {
	if v < c.min {
		return c.min
	}
	if v > c.max {
		return c.max
	}
	return v
}
