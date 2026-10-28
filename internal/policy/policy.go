// Package policy defines the abstraction a policy uses to decide whether the
// downloader's configuration should change.
//
// It deliberately contains no decision logic, no reward calculation, and no
// coupling to the downloader, fetcher, metrics, concurrency controller, or any
// learning library. Concrete adaptive policies and the experiment integration
// (including reward definition) live in later layers.
package policy

import (
	"errors"
	"fmt"
	"time"
)

var (
	// ErrInvalidAction indicates a malformed Action.
	ErrInvalidAction = errors.New("policy: invalid action")
	// ErrInvalidContext indicates a malformed Context.
	ErrInvalidContext = errors.New("policy: invalid context")
)

// ActionKind identifies what a policy wants to change.
//
// The zero value is ActionNone, so the zero Action is a valid "no change".
type ActionKind uint8

const (
	// ActionNone keeps the current configuration.
	ActionNone ActionKind = iota
	// ActionIncreaseConcurrency asks for one more concurrent request.
	ActionIncreaseConcurrency
	// ActionDecreaseConcurrency asks for one fewer concurrent request.
	ActionDecreaseConcurrency
	// ActionIncreaseChunkSize asks for a larger chunk size.
	ActionIncreaseChunkSize
	// ActionDecreaseChunkSize asks for a smaller chunk size.
	ActionDecreaseChunkSize
	// ActionSetConcurrency sets an explicit target concurrency (Action.Value).
	ActionSetConcurrency
	// ActionSetChunkSize sets an explicit target chunk size (Action.Value).
	ActionSetChunkSize
)

// Action is a policy decision. Value is only meaningful for the explicit
// ActionSetConcurrency and ActionSetChunkSize kinds and for the step magnitude of
// the increase/decrease kinds; it is ignored otherwise.
type Action struct {
	Kind  ActionKind
	Value int64
}

// NoChange returns an Action that keeps the current configuration.
func NoChange() Action { return Action{Kind: ActionNone} }

// IncreaseConcurrency returns an Action that adds one worker.
func IncreaseConcurrency() Action { return Action{Kind: ActionIncreaseConcurrency} }

// DecreaseConcurrency returns an Action that removes one worker.
func DecreaseConcurrency() Action { return Action{Kind: ActionDecreaseConcurrency} }

// IncreaseChunkSize returns an Action that enlarges the chunk size.
func IncreaseChunkSize() Action { return Action{Kind: ActionIncreaseChunkSize} }

// DecreaseChunkSize returns an Action that shrinks the chunk size.
func DecreaseChunkSize() Action { return Action{Kind: ActionDecreaseChunkSize} }

// SetConcurrency returns an Action that targets an explicit concurrency.
func SetConcurrency(target int) Action {
	return Action{Kind: ActionSetConcurrency, Value: int64(target)}
}

// SetChunkSize returns an Action that targets an explicit chunk size.
func SetChunkSize(target int64) Action {
	return Action{Kind: ActionSetChunkSize, Value: target}
}

// IsNoChange reports whether the action leaves the configuration unchanged.
func (a Action) IsNoChange() bool { return a.Kind == ActionNone }

// Validate reports whether the action is well formed. The zero Action is
// valid (it means "no change"); the Set* kinds require a positive Value.
func (a Action) Validate() error {
	switch a.Kind {
	case ActionNone,
		ActionIncreaseConcurrency,
		ActionDecreaseConcurrency,
		ActionIncreaseChunkSize,
		ActionDecreaseChunkSize:
		return nil
	case ActionSetConcurrency, ActionSetChunkSize:
		if a.Value <= 0 {
			return fmt.Errorf("%w: action kind %d requires a positive value, got %d", ErrInvalidAction, a.Kind, a.Value)
		}
		return nil
	default:
		return fmt.Errorf("%w: unknown kind %d", ErrInvalidAction, a.Kind)
	}
}

// String renders the action for logging and test output.
func (a Action) String() string {
	switch a.Kind {
	case ActionNone:
		return "none"
	case ActionIncreaseConcurrency:
		return "increase-concurrency"
	case ActionDecreaseConcurrency:
		return "decrease-concurrency"
	case ActionIncreaseChunkSize:
		return "increase-chunk-size"
	case ActionDecreaseChunkSize:
		return "decrease-chunk-size"
	case ActionSetConcurrency:
		return fmt.Sprintf("set-concurrency(%d)", a.Value)
	case ActionSetChunkSize:
		return fmt.Sprintf("set-chunk-size(%d)", a.Value)
	default:
		return fmt.Sprintf("unknown(%d)", a.Kind)
	}
}

// Context carries the observations a policy may use. It is plain data with no
// dependency on downloader types.
//
// Recent* fields describe the latest measurement window (or most recent
// sample); the counters describe the whole run so far.
//
// Example for a policy author: after the 10th attempt of a run with 4 workers,
// Concurrency is 4, ChunkSize is the target for the NEXT allocation (not the
// size of the attempt just finished), Throughput is the per-request rate over
// the last window, AggregateThroughput is the wall-clock rate of that same
// window, and SuccessfulRequests/FailedRequests/Retries/TotalAttempts are
// cumulative. A policy that wants to know "did adding a worker help?" must
// compare AggregateThroughput across decisions, not Throughput.
type Context struct {
	// Current configuration.
	Concurrency int
	ChunkSize   int64

	// Recent observations.
	// Throughput is the windowed PER-REQUEST throughput: total bytes over
	// total request durations for the last window of attempts. It reflects the
	// rate a single request achieves and is largely independent of how many
	// requests run concurrently.
	Throughput float64 // bytes per second (per-request/windowed)
	// AggregateThroughput is the windowed AGGREGATE downloader throughput: the
	// total bytes over the last window of attempts divided by the WALL-CLOCK
	// span covering those attempts. With N overlapping requests this reflects
	// the achieved N-way parallelism, so it is sensitive to concurrency
	// changes, unlike Throughput. Zero when the window is empty or the span is
	// non-positive. It never replaces Throughput.
	AggregateThroughput float64       // bytes per second (aggregate/wall-clock)
	RequestDuration     time.Duration // duration of the most recent request
	// Latency is the mean time until response headers arrived, over the
	// attempts in the recent window that received a response.
	Latency          time.Duration
	BytesTransferred int64 // bytes of the most recent request

	// Outcome counters.
	SuccessfulRequests int64
	FailedRequests     int64
	Retries            int64
	TotalAttempts      int64
}

// Validate reports whether the context is well formed. All zero values are
// valid; only negative observations are rejected.
func (c Context) Validate() error {
	switch {
	case c.Concurrency < 0:
		return fmt.Errorf("%w: negative concurrency %d", ErrInvalidContext, c.Concurrency)
	case c.ChunkSize < 0:
		return fmt.Errorf("%w: negative chunk size %d", ErrInvalidContext, c.ChunkSize)
	case c.Throughput < 0:
		return fmt.Errorf("%w: negative throughput %v", ErrInvalidContext, c.Throughput)
	case c.AggregateThroughput < 0:
		return fmt.Errorf("%w: negative aggregate throughput %v", ErrInvalidContext, c.AggregateThroughput)
	case c.RequestDuration < 0:
		return fmt.Errorf("%w: negative request duration %v", ErrInvalidContext, c.RequestDuration)
	case c.Latency < 0:
		return fmt.Errorf("%w: negative latency %v", ErrInvalidContext, c.Latency)
	case c.BytesTransferred < 0:
		return fmt.Errorf("%w: negative bytes transferred %d", ErrInvalidContext, c.BytesTransferred)
	case c.SuccessfulRequests < 0:
		return fmt.Errorf("%w: negative successful requests %d", ErrInvalidContext, c.SuccessfulRequests)
	case c.FailedRequests < 0:
		return fmt.Errorf("%w: negative failed requests %d", ErrInvalidContext, c.FailedRequests)
	case c.Retries < 0:
		return fmt.Errorf("%w: negative retries %d", ErrInvalidContext, c.Retries)
	case c.TotalAttempts < 0:
		return fmt.Errorf("%w: negative total attempts %d", ErrInvalidContext, c.TotalAttempts)
	}
	return nil
}

// Policy decides how the downloader configuration should change. Reward
// calculation is intentionally outside this interface: the experiment layer
// supplies a reward so different policies can be compared against the same
// definition.
type Policy interface {
	// SelectAction returns the action to take for the given context.
	SelectAction(ctx Context) Action
	// Update reports the reward observed after applying action in ctx.
	Update(ctx Context, action Action, reward float64)
}

// FixedPolicy always chooses "no change". It is the non-adaptive baseline.
type FixedPolicy struct{}

// SelectAction always returns NoChange.
func (FixedPolicy) SelectAction(Context) Action { return NoChange() }

// Update is a no-op.
func (FixedPolicy) Update(Context, Action, float64) {}

var _ Policy = FixedPolicy{}
