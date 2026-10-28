// Package concurrency provides a small, policy-free controller for the desired
// number of concurrently active requests.
//
// It represents only the target level: it never creates or destroys
// goroutines. Deciding when to increase or decrease belongs to a future policy
// layer, and translating the target into actual workers belongs to the
// downloader.
package concurrency

import (
	"errors"
	"fmt"
	"sync"
)

// ErrInvalidConfig indicates an unusable [min, max] configuration.
var ErrInvalidConfig = errors.New("concurrency: invalid configuration")

// Controller tracks a target concurrency bounded by [min, max].
//
// It is safe for concurrent use.
type Controller struct {
	mu      sync.Mutex
	current int
	min     int
	max     int
}

// New returns a Controller whose target starts at min. It returns
// ErrInvalidConfig unless min >= 1 and max >= min.
func New(min, max int) (*Controller, error) {
	if min < 1 {
		return nil, fmt.Errorf("%w: min must be >= 1, got %d", ErrInvalidConfig, min)
	}
	if max < min {
		return nil, fmt.Errorf("%w: max must be >= min (%d), got %d", ErrInvalidConfig, min, max)
	}
	return &Controller{current: min, min: min, max: max}, nil
}

// Current returns the current target concurrency.
func (c *Controller) Current() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.current
}

// Min returns the configured minimum concurrency.
func (c *Controller) Min() int { return c.min }

// Max returns the configured maximum concurrency.
func (c *Controller) Max() int { return c.max }

// Increase raises the target by one, up to max, and returns the new target.
func (c *Controller) Increase() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.current < c.max {
		c.current++
	}
	return c.current
}

// Decrease lowers the target by one, down to min, and returns the new target.
func (c *Controller) Decrease() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.current > c.min {
		c.current--
	}
	return c.current
}

// Set clamps target to [min, max], stores it, and returns the stored value.
//
// Set exists for explicit-target policies and for tests/tools that need to place
// the controller directly (the deterministic policies use Increase/Decrease).
// It never touches in-flight requests: the pool reads Current() only when it
// allocates, so a smaller target drains by letting workers retire between
// requests.
func (c *Controller) Set(target int) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.current = c.clamp(target)
	return c.current
}

func (c *Controller) clamp(v int) int {
	if v < c.min {
		return c.min
	}
	if v > c.max {
		return c.max
	}
	return v
}
