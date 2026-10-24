package downloader

import (
	"sync"
	"time"

	"adaptivedl/internal/chunksize"
	"adaptivedl/internal/concurrency"
	"adaptivedl/internal/metrics"
	"adaptivedl/internal/policy"
	"adaptivedl/internal/rl"
)

// defaultPolicyWindow is the number of most recent attempts used to compute
// recent throughput when Config.PolicyWindowSize is not set.
const defaultPolicyWindow = 8

// policyRunner serializes policy decisions and applies the resulting actions to
// the concurrency and chunk-size controllers.
//
// The policy interface is intentionally not concurrency-safe: every
// SelectAction/Update call happens under mu, so the sequence
// observe -> SelectAction -> apply -> Update is atomic from the policy's view.
// This is the single decision path for all ActionKinds.
type policyRunner struct {
	policy          policy.Policy
	controller      *concurrency.Controller
	chunkController *chunksize.Controller
	fixedChunkSize  int64
	window          int
	apply           func(target int)
	rewarder        *rl.RewardCalculator
	// now returns the current wall-clock time. It is a field so tests can
	// supply a deterministic clock for the wall-clock aggregate throughput.
	now func() time.Time

	mu         sync.Mutex
	recent     []attemptSample
	successful int64
	failed     int64
	retries    int64
	attempts   int64

	// Credit-assignment state. prevContext/prevAction record the observation
	// the previous decision was made at, and the action chosen there. When the
	// next observation arrives, the reward for prevAction is computed from the
	// change between prevContext and the new context.
	prevContext    policy.Context
	prevAction     policy.Action
	havePrevAction bool
	havePrevObs    bool
}

type attemptSample struct {
	bytes    int64
	duration time.Duration
	latency  time.Duration
	// end is the wall-clock time the attempt was observed to complete. It is
	// used only to compute the aggregate (wall-clock) throughput over the
	// window; start is derived as end-duration.
	end time.Time
}

func newPolicyRunner(
	p policy.Policy,
	controller *concurrency.Controller,
	chunkController *chunksize.Controller,
	fixedChunkSize int64,
	window int,
	apply func(int),
	rewarder *rl.RewardCalculator,
) *policyRunner {
	if window <= 0 {
		window = defaultPolicyWindow
	}
	return &policyRunner{
		policy:          p,
		controller:      controller,
		chunkController: chunkController,
		fixedChunkSize:  fixedChunkSize,
		window:          window,
		apply:           apply,
		rewarder:        rewarder,
		now:             time.Now,
	}
}

// currentChunkSize returns the authoritative current target chunk size, read
// from the controller at call time (or the fixed size when none is configured).
func (r *policyRunner) currentChunkSize() int64 {
	if r.chunkController != nil {
		return r.chunkController.Current()
	}
	return r.fixedChunkSize
}

// observe records one completed attempt, runs a single serialized policy
// decision, and applies the resulting action.
//
// Two throughput observations are computed over the last `window` attempts:
//
//   - Throughput (per-request): sum(bytes) / sum(durations). This is the rate a
//     single request achieved and does not grow with concurrency.
//   - AggregateThroughput (wall-clock): sum(bytes) / (latest end - earliest
//     start). With overlapping requests the wall-clock span stays near one
//     request duration while the bytes add up, so this reflects achieved
//     parallelism and responds to concurrency changes.
func (r *policyRunner) observe(rec metrics.Record) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.attempts++
	if rec.Success {
		r.successful++
	} else {
		r.failed++
	}
	if rec.Attempt > 1 {
		r.retries++
	}

	r.recent = append(r.recent, attemptSample{bytes: rec.Bytes, duration: rec.Duration, latency: rec.Latency, end: r.now()})
	if len(r.recent) > r.window {
		r.recent = r.recent[len(r.recent)-r.window:]
	}

	var (
		bytes      int64
		elapsed    time.Duration
		latencySum time.Duration
		latencyN   int64
	)
	for _, s := range r.recent {
		bytes += s.bytes
		elapsed += s.duration
		if s.latency > 0 {
			latencySum += s.latency
			latencyN++
		}
	}
	var latency time.Duration
	if latencyN > 0 {
		latency = latencySum / time.Duration(latencyN)
	}
	var throughput float64
	if elapsed > 0 {
		throughput = float64(bytes) / elapsed.Seconds()
	}

	ctx := policy.Context{
		Concurrency:         r.controller.Current(),
		ChunkSize:           r.currentChunkSize(),
		Throughput:          throughput,
		AggregateThroughput: r.aggregateThroughput(),
		RequestDuration:     rec.Duration,
		Latency:             latency,
		BytesTransferred:    rec.Bytes,
		SuccessfulRequests:  r.successful,
		FailedRequests:      r.failed,
		Retries:             r.retries,
		TotalAttempts:       r.attempts,
	}

	// Credit assignment. The outcome just observed is the consequence of the
	// PREVIOUS action, so the reward is computed between the previous context
	// and this one, and Update is called with the previous (context, action).
	// This keeps the learner from being credited/debited for the new action
	// before its outcome exists.
	if r.havePrevAction {
		reward := r.calculateReward(ctx)
		r.policy.Update(r.prevContext, r.prevAction, reward)
	}

	// Now select and apply the action for the context just observed. Its
	// reward will be computed on the next observation.
	action := r.policy.SelectAction(ctx)
	r.applyAction(action)

	r.prevContext = ctx
	r.prevAction = action
	r.havePrevAction = true
	r.havePrevObs = true
}

// aggregateThroughput returns the wall-clock aggregate throughput over the
// current recent-attempt window: total bytes divided by the span from the
// earliest attempt start to the latest attempt end. It must be called with r.mu
// held. It returns 0 for an empty window or a non-positive span.
func (r *policyRunner) aggregateThroughput() float64 {
	if len(r.recent) == 0 {
		return 0
	}
	first := r.recent[0]
	earliest := first.end.Add(-first.duration)
	latest := first.end
	var bytes int64
	for _, s := range r.recent {
		bytes += s.bytes
		if start := s.end.Add(-s.duration); start.Before(earliest) {
			earliest = start
		}
		if s.end.After(latest) {
			latest = s.end
		}
	}
	span := latest.Sub(earliest)
	if span <= 0 {
		return 0
	}
	return float64(bytes) / span.Seconds()
}

// calculateReward computes the scalar reward for the previous action from the
// change between the previous and current observations. It returns 0 when no
// reward calculator is configured or when the previous action is not part of
// the RL action space (the deterministic policies ignore reward entirely).
func (r *policyRunner) calculateReward(current policy.Context) float64 {
	if r.rewarder == nil {
		return 0
	}
	prevState, err := rl.StateFromContext(r.prevContext, r.rewarderConfig())
	if err != nil {
		return 0
	}
	curState, err := rl.StateFromContext(current, r.rewarderConfig())
	if err != nil {
		return 0
	}
	actionIndex, err := rl.ActionIndexFromAction(r.prevAction)
	if err != nil {
		// Reserved/unsupported actions (e.g. Set*) have no learning arm.
		return 0
	}
	reward := r.rewarder.Calculate(prevState, curState, actionIndex, r.havePrevObs)
	return reward.Value
}

// rewarderConfig returns the normalization Config used to convert Contexts into
// RL States for reward computation. It is derived from the runner's configured
// bounds so reward and learner normalization agree.
func (r *policyRunner) rewarderConfig() rl.Config {
	return rl.Config{
		MinConcurrency:     r.controller.Min(),
		MaxConcurrency:     r.controller.Max(),
		MinChunkSize:       1,
		MaxChunkSize:       r.maxChunkSize(),
		MaxThroughput:      1e9,
		MaxRequestDuration: 1 << 40,
		CounterScale:       1000,
	}
}

func (r *policyRunner) maxChunkSize() int64 {
	if r.chunkController != nil {
		return r.chunkController.Max()
	}
	if r.fixedChunkSize > 0 {
		return r.fixedChunkSize
	}
	return 1
}

// actionStep returns the number of unit controller steps an increase/decrease
// action applies. Value <= 0 preserves the historical single-step behavior used
// by v1-v6 policies; v7 uses explicit step magnitudes (e.g. 4).
func actionStep(a policy.Action) int {
	if a.Value <= 0 {
		return 1
	}
	if a.Value > 16 {
		return 16
	}
	return int(a.Value)
}

// applyAction translates a generic policy Action into a controller change.
// Each ActionKind affects only its corresponding controller: concurrency
// actions never touch chunk size, and chunk-size actions never touch
// concurrency. Explicit-target actions remain reserved and are ignored.
func (r *policyRunner) applyAction(action policy.Action) {
	switch action.Kind {
	case policy.ActionNone:
		return
	case policy.ActionIncreaseConcurrency:
		for i := 0; i < actionStep(action); i++ {
			r.controller.Increase()
		}
	case policy.ActionDecreaseConcurrency:
		for i := 0; i < actionStep(action); i++ {
			r.controller.Decrease()
		}
	case policy.ActionIncreaseChunkSize:
		if r.chunkController == nil {
			return
		}
		r.chunkController.Increase()
		return
	case policy.ActionDecreaseChunkSize:
		if r.chunkController == nil {
			return
		}
		r.chunkController.Decrease()
		return
	case policy.ActionSetConcurrency, policy.ActionSetChunkSize:
		// Explicit-target actions are reserved for a later step.
		return
	default:
		return
	}

	// Only concurrency changes notify the worker pool; the pool reads the
	// chunk-size controller at each allocation, so no notification is needed.
	if r.apply != nil {
		r.apply(r.controller.Current())
	}
}
