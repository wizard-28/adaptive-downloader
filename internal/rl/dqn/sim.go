package dqn

import (
	"errors"
	"fmt"
	"math"
	"math/rand"
	"time"
)

// ErrEnvironment indicates an unusable environment configuration or call.
var ErrEnvironment = errors.New("dqn: environment error")

// SimConfig parameterizes the fast controlled training environment. It is a
// deliberately simple analytical model of a single-source Range download under
// variable conditions. It is NOT the real downloader; it exists to train and
// sanity-check the DQN controller. Training conditions here are NOT the final
// evaluation conditions.
type SimConfig struct {
	FileSize int64

	MinConcurrency int
	MaxConcurrency int
	MinChunkSize   int64
	MaxChunkSize   int64
	ChunkStep      int64

	// Bandwidth is the server-side per-response pacing rate (bytes/sec).
	Bandwidth int64
	// Bandwidth2 and SwitchAfter model a rate transition: after SwitchAfter
	// simulated requests the rate becomes Bandwidth2. Zero SwitchAfter disables.
	Bandwidth2  int64
	SwitchAfter int

	// Latency is added to every request.
	Latency time.Duration
	// LossPercent is the per-request failure probability (0..100).
	LossPercent float64

	// MaxEpisodeSteps bounds an episode. 0 disables truncation.
	MaxEpisodeSteps int
	// DecisionCost simulates fixed per-decision overhead in the environment
	// (kept 0 by default; documented as an assumption).
	DecisionCost time.Duration

	Spec StateSpec

	// rng is injected for determinism.
	rng *rand.Rand
}

// SimEnv is a deterministic, fast episodic simulator implementing Environment.
type SimEnv struct {
	cfg SimConfig

	concurrency int
	chunkSize   int64

	totalBytes int64
	doneBytes  int64

	requests  int // completed request count (for rate switching)
	steps     int
	done      bool
	truncated bool
	elapsed   time.Duration

	// temporal trackers / counters
	prevThroughput float64
	throughputEWMA float64
	throughputVol  float64
	prevDuration   float64
	sinceConc      time.Duration
	sinceChunk     time.Duration
	failWindow     []int // 1 = failed attempt, 0 = success
	retryWindow    []int
}

// NewSimEnv validates cfg and returns a simulator using the injected seed.
func NewSimEnv(cfg SimConfig, seed int64) (*SimEnv, error) {
	if cfg.FileSize <= 0 {
		return nil, fmt.Errorf("%w: file size must be > 0", ErrEnvironment)
	}
	if cfg.MinConcurrency < 1 || cfg.MaxConcurrency < cfg.MinConcurrency {
		return nil, fmt.Errorf("%w: invalid concurrency bounds", ErrEnvironment)
	}
	if cfg.MinChunkSize < 1 || cfg.MaxChunkSize < cfg.MinChunkSize || cfg.ChunkStep < 1 {
		return nil, fmt.Errorf("%w: invalid chunk bounds", ErrEnvironment)
	}
	if cfg.Bandwidth <= 0 {
		return nil, fmt.Errorf("%w: bandwidth must be > 0", ErrEnvironment)
	}
	if cfg.Bandwidth2 < 0 || cfg.SwitchAfter < 0 {
		return nil, fmt.Errorf("%w: invalid rate transition", ErrEnvironment)
	}
	if cfg.LossPercent < 0 || cfg.LossPercent > 100 {
		return nil, fmt.Errorf("%w: loss percent must be in [0,100]", ErrEnvironment)
	}
	if cfg.MaxEpisodeSteps < 0 {
		return nil, fmt.Errorf("%w: max episode steps must be >= 0", ErrEnvironment)
	}
	if cfg.Spec != (StateSpec{}) {
		if err := cfg.Spec.Validate(); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrEnvironment, err)
		}
	} else {
		cfg.Spec = DefaultStateSpec()
	}
	cfg.rng = rand.New(rand.NewSource(seed))
	return &SimEnv{cfg: cfg, chunkSize: cfg.MinChunkSize}, nil
}

// MaxEpisodeSteps returns the configured truncation bound.
func (e *SimEnv) MaxEpisodeSteps() int { return e.cfg.MaxEpisodeSteps }

// Done reports episode termination.
func (e *SimEnv) Done() bool { return e.done || e.truncated }

func (e *SimEnv) currentRate() int64 {
	if e.cfg.Bandwidth2 > 0 && e.cfg.SwitchAfter > 0 && e.requests >= e.cfg.SwitchAfter {
		return e.cfg.Bandwidth2
	}
	return e.cfg.Bandwidth
}

// Reset begins a new episode.
func (e *SimEnv) Reset() ([]float64, error) {
	e.concurrency = e.cfg.MinConcurrency + e.cfg.rng.Intn(e.cfg.MaxConcurrency-e.cfg.MinConcurrency+1)
	e.chunkSize = e.cfg.MinChunkSize
	e.totalBytes = e.cfg.FileSize
	e.doneBytes = 0
	e.requests = 0
	e.steps = 0
	e.done = false
	e.truncated = false
	e.elapsed = 0
	e.prevThroughput, e.throughputEWMA, e.throughputVol = 0, 0, 0
	e.prevDuration = 0
	e.sinceConc, e.sinceChunk = 0, 0
	e.failWindow, e.retryWindow = nil, nil
	return e.State().Features(e.cfg.Spec)
}

// State returns the current raw observation.
func (e *SimEnv) State() Observation {
	thr := e.currentThroughput()
	return Observation{
		Concurrency:         e.concurrency,
		ChunkSize:           e.chunkSize,
		TotalBytes:          e.totalBytes,
		DoneBytes:           e.doneBytes,
		AggregateThroughput: thr,
		RequestDuration:     time.Duration(e.prevDuration * float64(time.Second)),
		FailedRequests:      int64(countOnes(e.failWindow)),
		Retries:             int64(countOnes(e.retryWindow)),
		TotalAttempts:       int64(e.requests),
		ThroughputEWMA:      e.throughputEWMA,
		PrevThroughput:      e.prevThroughput,
		ThroughputVol:       e.throughputVol,
		PrevDuration:        time.Duration(e.prevDuration * float64(time.Second)),
		SinceConcChange:     e.sinceConc,
		SinceChunkChange:    e.sinceChunk,
		WindowFailures:      int64(countOnes(e.failWindow)),
		WindowRetries:       int64(countOnes(e.retryWindow)),
		WindowAttempts:      int64(len(e.failWindow)),
	}
}

// currentThroughput estimates aggregate throughput as concurrency * per-request
// rate (the analytical model's aggregate view).
func (e *SimEnv) currentThroughput() float64 {
	rate := float64(e.currentRate())
	c := float64(e.concurrency)
	// latency amortization: effective per-request rate declines with latency.
	return c * rate / (1 + rate*(e.cfg.Latency.Seconds())/float64(e.chunkSize))
}

// Step applies action, advances the episode, and returns the outcome.
func (e *SimEnv) Step(action int) (StepResult, []float64, error) {
	if e.done || e.truncated {
		return StepResult{}, nil, fmt.Errorf("%w: episode already finished", ErrEnvironment)
	}
	if action < 0 || action >= NumActions() {
		return StepResult{}, nil, fmt.Errorf("%w: action %d", ErrInvalidAction, action)
	}

	beforeC, beforeK := e.concurrency, e.chunkSize
	applySimAction(&e.concurrency, &e.chunkSize, action, e.cfg)
	effective, noop := action, false
	switch {
	case action == 0:
		noop = true
	case action == 1 || action == 2:
		if e.concurrency == beforeC {
			noop = true
		}
	case action == 3 || action == 4:
		if e.chunkSize == beforeK {
			noop = true
		}
	}
	if e.concurrency != beforeC {
		e.sinceConc = 0
	}
	if e.chunkSize != beforeK {
		e.sinceChunk = 0
	}

	// Advance the environment by one "request wave": each worker completes one
	// request of chunkSize at the current rate (or fails with loss probability).
	elapsed := e.cfg.DecisionCost
	bytesThisWave := int64(0)
	failures, retries := 0, 0
	for w := 0; w < e.concurrency && e.doneBytes < e.totalBytes; w++ {
		e.requests++
		failed := e.cfg.rng.Float64()*100 < e.cfg.LossPercent
		rate := float64(e.currentRate())
		svc := e.cfg.Latency + time.Duration(float64(e.chunkSize)/rate*float64(time.Second))
		elapsed += svc
		if failed {
			failures++
			retries++ // a failed attempt is retried once in this model
			svc2 := e.cfg.Latency + time.Duration(float64(e.chunkSize)/rate*float64(time.Second))
			elapsed += svc2
		}
		// completed bytes: remaining clamped to chunk size
		remaining := e.totalBytes - e.doneBytes
		n := e.chunkSize
		if n > remaining {
			n = remaining
		}
		e.doneBytes += n
		bytesThisWave += n
		if e.doneBytes >= e.totalBytes {
			break
		}
	}

	// Update temporal trackers.
	dt := elapsed.Seconds()
	e.elapsed += elapsed
	e.sinceConc += elapsed
	e.sinceChunk += elapsed
	var thr float64
	if dt > 0 {
		thr = float64(bytesThisWave) / dt
	}
	e.prevThroughput = thr
	e.throughputEWMA = e.cfg.Spec.EWMAAlpha*thr + (1-e.cfg.Spec.EWMAAlpha)*e.throughputEWMA
	e.throughputVol = e.cfg.Spec.EWMAAlpha*math.Abs(thr-e.prevThroughput) + (1-e.cfg.Spec.EWMAAlpha)*e.throughputVol
	e.prevDuration = dt
	pushWindow(&e.failWindow, boolToInt(failures > 0), e.cfg.Spec.CounterWindow)
	pushWindow(&e.retryWindow, boolToInt(retries > 0), e.cfg.Spec.CounterWindow)

	e.steps++
	if e.doneBytes >= e.totalBytes {
		e.done = true
	}
	if e.cfg.MaxEpisodeSteps > 0 && e.steps >= e.cfg.MaxEpisodeSteps && !e.done {
		e.truncated = true
	}

	reward := -elapsed.Seconds()
	next, err := e.State().Features(e.cfg.Spec)
	if err != nil {
		return StepResult{}, nil, err
	}
	res := StepResult{
		State:          e.State(),
		Elapsed:        elapsed,
		Reward:         reward,
		Done:           e.done,
		Truncated:      e.truncated,
		Requested:      action,
		Effective:      effective,
		NoOp:           noop,
		Concurrency:    e.concurrency,
		ChunkSize:      e.chunkSize,
		RemainingBytes: e.totalBytes - e.doneBytes,
	}
	return res, next, nil
}

func applySimAction(c *int, k *int64, action int, cfg SimConfig) {
	switch action {
	case 1:
		if *c < cfg.MaxConcurrency {
			*c++
		}
	case 2:
		if *c > cfg.MinConcurrency {
			*c--
		}
	case 3:
		if *k+cfg.ChunkStep <= cfg.MaxChunkSize {
			*k += cfg.ChunkStep
		}
	case 4:
		if *k-cfg.ChunkStep >= cfg.MinChunkSize {
			*k -= cfg.ChunkStep
		}
	}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func countOnes(v []int) int {
	n := 0
	for _, x := range v {
		if x == 1 {
			n++
		}
	}
	return n
}

func pushWindow(w *[]int, v, cap int) {
	*w = append(*w, v)
	if len(*w) > cap {
		*w = (*w)[len(*w)-cap:]
	}
}

// NumActions returns the DQN action-space size.
func NumActions() int { return len(ActionNames()) }
