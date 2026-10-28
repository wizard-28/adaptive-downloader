package dqn

// LivePolicy adapts a frozen DQN Model to the existing downloader policy.Policy
// interface. The downloader's serialized policy runner calls SelectAction once
// per completed attempt with a policy.Context; this adapter maintains the
// incremental temporal trackers (EWMA, slope, volatility, window rates,
// staleness) needed to build the DQN Observation, then performs deterministic
// greedy inference (epsilon = 0).
//
// It does not modify the downloader, controllers, or the LinUCB path.

import (
	"fmt"
	"sync"
	"time"

	"adaptivedl/internal/policy"
)

// LivePolicy is a policy.Policy backed by a frozen DQN model.
type LivePolicy struct {
	model *Model
	spec  StateSpec

	mu sync.Mutex

	havePrev bool

	prevAggregate float64
	ewma          float64
	vol           float64
	prevDuration  time.Duration

	sinceConc  time.Duration
	sinceChunk time.Duration

	prevConc  int
	prevChunk int64
	att       int

	failWindow      []int
	retryWindow     []int
	completedWindow []int
	prevFailures    int64
	prevRetries     int64
	prevAttempts    int64
	prevSuccesses   int64

	// Log records every decision for live observability.
	log []Decision

	// concurrencyOnly selects the concurrency-only action space (Candidate v4:
	// chunk size fixed at 1 MiB). The historical v1/v2/v3 models use the full
	// five-action space.
	concurrencyOnly bool

	// stateV5 selects the v5 (state spec version 2) observation layout. The
	// historical v1-v4 models use the v1 layout.
	stateV5 bool

	// stateV6 selects the v6 (state spec version 3) observation layout. The raw
	// layout matches v5; only the concurrency normalization bounds differ.
	stateV6 bool

	// v5 window tracks completed requests in addition to failures/retries.
	v5Completed int64

	// totalBytes is the file size, needed for remaining-bytes fraction. It is
	// captured from the context on the first call if not set explicitly.
	totalBytes int64

	// doneBytesAccum is the sum of BytesTransferred over successful attempts
	// (each success wrote exactly one completed range); it is the honest
	// progress observable, unlike a successes x last-bytes estimate.
	doneBytesAccum int64
}

// Decision is one live-policy decision record (observability).
type Decision struct {
	Index               int           `json:"index"`
	Action              string        `json:"action"`
	ActionIndex         int           `json:"action_index"`
	Reward              float64       `json:"reward"`
	ConcurrencyBefore   int           `json:"concurrency_before"`
	ConcurrencyAfter    int           `json:"concurrency_after"`
	ChunkBefore         int64         `json:"chunk_before"`
	ChunkAfter          int64         `json:"chunk_after"`
	AggregateThroughput float64       `json:"aggregate_throughput"`
	RequestDuration     time.Duration `json:"request_duration"`
	RemainingBytes      int64         `json:"remaining_bytes"`
	Effective           bool          `json:"effective"`
	NoOp                bool          `json:"no_op"`
	Features            []float64     `json:"features"`
}

// NewLivePolicy wraps a validated model. totalBytes is the download size (for
// the remaining-bytes feature); pass 0 if unknown, in which case progress is
// still accumulated but the remaining-bytes fraction cannot be derived.
func NewLivePolicy(m *Model, totalBytes int64) (*LivePolicy, error) {
	if m == nil {
		return nil, ErrInvalidModel
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &LivePolicy{model: m, spec: m.StateSpec, totalBytes: totalBytes}, nil
}

// NewLiveConcurrencyPolicy wraps a concurrency-only model (Candidate v4). The
// model must have a 3-action output space.
func NewLiveConcurrencyPolicy(m *Model, totalBytes int64) (*LivePolicy, error) {
	if m == nil {
		return nil, ErrInvalidModel
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	if m.NumActions != NumConcurrencyActions {
		return nil, fmt.Errorf("%w: concurrency-only policy needs %d actions, model has %d", ErrInvalidModel, NumConcurrencyActions, m.NumActions)
	}
	return &LivePolicy{model: m, spec: m.StateSpec, totalBytes: totalBytes, concurrencyOnly: true}, nil
}

// NewLiveConcurrencyPolicyV5 wraps a Candidate v5 concurrency-only model whose
// state uses spec version 2. The model must have a 3-action output space and a
// v2 state spec.
func NewLiveConcurrencyPolicyV5(m *Model, totalBytes int64) (*LivePolicy, error) {
	if m == nil {
		return nil, ErrInvalidModel
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	if m.NumActions != NumConcurrencyActions {
		return nil, fmt.Errorf("%w: concurrency-only policy needs %d actions, model has %d", ErrInvalidModel, NumConcurrencyActions, m.NumActions)
	}
	if m.StateSpec.Version != StateSpecVersionV5 {
		return nil, fmt.Errorf("%w: v5 policy needs state spec version %d, model has %d", ErrInvalidModel, StateSpecVersionV5, m.StateSpec.Version)
	}
	return &LivePolicy{model: m, spec: m.StateSpec, totalBytes: totalBytes, concurrencyOnly: true, stateV5: true}, nil
}

// NewLiveConcurrencyPolicyV6 wraps a Candidate v6 concurrency-only model whose
// state uses spec version 3. The model must have a 3-action output space and a
// v3 state spec.
func NewLiveConcurrencyPolicyV6(m *Model, totalBytes int64) (*LivePolicy, error) {
	if m == nil {
		return nil, ErrInvalidModel
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	if m.NumActions != NumConcurrencyActions {
		return nil, fmt.Errorf("%w: concurrency-only policy needs %d actions, model has %d", ErrInvalidModel, NumConcurrencyActions, m.NumActions)
	}
	if m.StateSpec.Version != StateSpecVersionV6 {
		return nil, fmt.Errorf("%w: v6 policy needs state spec version %d, model has %d", ErrInvalidModel, StateSpecVersionV6, m.StateSpec.Version)
	}
	return &LivePolicy{model: m, spec: m.StateSpec, totalBytes: totalBytes, concurrencyOnly: true, stateV6: true}, nil
}

// SelectAction builds the DQN observation, runs greedy inference, records the
// decision, and returns the existing policy.Action for the runner to apply.
//
// The three state layouts (v1, v5, v6) share the same action decoding: an index
// becomes a generic policy.Action, and the downloader decides what it means. A
// model error falls back to NoChange so a malformed observation can never crash
// a live run; the fallback is visible in the decision log as a "none" action.
func (p *LivePolicy) SelectAction(ctx policy.Context) policy.Action {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.stateV5 {
		return p.selectActionV5(ctx)
	}
	if p.stateV6 {
		return p.selectActionV6(ctx)
	}

	obs := p.observe(ctx)
	idx, err := p.model.SelectAction(obs)
	if err != nil {
		return policy.NoChange()
	}
	p.record(idx, obs, ctx)
	p.prevConc, p.prevChunk = ctx.Concurrency, ctx.ChunkSize
	p.prevAttempts = ctx.TotalAttempts
	p.att++
	action, err := p.actionFromIndex(idx)
	if err != nil {
		return policy.NoChange()
	}
	return action
}

// selectActionV5 mirrors SelectAction for the v5 (state version 2) layout. It
// maintains the same temporal trackers and performs deterministic greedy
// inference. Must be called under p.mu.
func (p *LivePolicy) selectActionV5(ctx policy.Context) policy.Action {
	obs := p.observeV5(ctx)
	idx, err := p.model.SelectActionV5(obs)
	if err != nil {
		return policy.NoChange()
	}
	p.recordV5(idx, obs, ctx)
	p.prevConc, p.prevChunk = ctx.Concurrency, ctx.ChunkSize
	p.prevAttempts = ctx.TotalAttempts
	p.att++
	action, err := p.actionFromIndex(idx)
	if err != nil {
		return policy.NoChange()
	}
	return action
}

// selectActionV6 mirrors selectActionV5 for the v6 (state version 3) layout.
// The raw 13-feature layout is identical; the spec carries the [1,16] bounds.
// Must be called under p.mu.
func (p *LivePolicy) selectActionV6(ctx policy.Context) policy.Action {
	obs := p.observeV5(ctx)
	idx, err := p.model.SelectActionV6(obs)
	if err != nil {
		return policy.NoChange()
	}
	p.recordV6(idx, obs, ctx)
	p.prevConc, p.prevChunk = ctx.Concurrency, ctx.ChunkSize
	p.prevAttempts = ctx.TotalAttempts
	p.att++
	action, err := p.actionFromIndex(idx)
	if err != nil {
		return policy.NoChange()
	}
	return action
}

// actionFromIndex decodes an action index using the policy's action space.
func (p *LivePolicy) actionFromIndex(idx int) (policy.Action, error) {
	if p.concurrencyOnly {
		return ConcurrencyIndexToAction(idx)
	}
	return IndexToAction(idx)
}

// actionName renders an action index using the policy's action space.
func (p *LivePolicy) actionName(idx int) string {
	if p.concurrencyOnly {
		if idx >= 0 && idx < len(ConcurrencyActionNames()) {
			return ConcurrencyActionNames()[idx]
		}
		return "invalid"
	}
	if idx >= 0 && idx < len(ActionNames()) {
		return ActionNames()[idx]
	}
	return "invalid"
}

// Update applies the runner's scalar reward to the previous decision and
// updates the staleness/window trackers for the resolved action effect. The
// reward is informational here (inference-only); it is recorded for analysis.
//
// A frozen model is not trained live, so there is nothing to learn from the
// reward: this method exists to satisfy policy.Policy and to document that
// absence explicitly. Training happens offline in python/ and the result is
// loaded as a fixed artifact.
func (p *LivePolicy) Update(ctx policy.Context, action policy.Action, reward float64) {}

// observe builds a DQN Observation from the downloader context and advances the
// temporal trackers. It must be called under p.mu.
//
// This is where the live v6 (state version 3) input is assembled: thirteen
// features, all from the downloader's own measurements.
//
//	concurrency_norm            N / 16
//	remaining_bytes_frac        1 - bytes written / file size
//	aggregate_throughput_norm   wall-clock rate / 8 MiB/s
//	throughput EWMA, slope       smoothed rate and its change
//	throughput volatility        EWMA of |rate - previous rate|
//	request_duration_*           last duration and its change
//	recent_failure/retry rate    share of the last 8 decisions/attempts with a
//	                             failure or a retry
//	since_concurrency_change    staleness of the N target
//	recent_completed            completions in the window
//	throughput_improvement      current rate - previous rate
//
// Progress uses doneBytesAccum (bytes of successful attempts), not
// successes x last-bytes, so a mixed-size run cannot report false progress.
// The trackers advance after the snapshot, so one observation never sees its own
// update.
func (p *LivePolicy) observe(ctx policy.Context) Observation {
	thr := ctx.AggregateThroughput

	// window rates from cumulative counters
	failDelta := ctx.FailedRequests - p.prevFailures
	retryDelta := ctx.Retries - p.prevRetries
	attDelta := ctx.TotalAttempts - p.prevAttempts
	if attDelta < 0 {
		attDelta = 0
	}
	succDelta := ctx.SuccessfulRequests - p.prevSuccesses
	if succDelta > 0 {
		p.doneBytesAccum += ctx.BytesTransferred
	}
	p.pushWindow(&p.failWindow, boolToInt(failDelta > 0))
	p.pushWindow(&p.retryWindow, boolToInt(retryDelta > 0))

	// staleness: count decisions since an effective change (approximate in
	// decision units, converted using the last request duration).
	step := ctx.RequestDuration
	if step <= 0 {
		step = time.Millisecond
	}
	if ctx.Concurrency == p.prevConc {
		p.sinceConc += step
	} else {
		p.sinceConc = 0
	}
	if ctx.ChunkSize == p.prevChunk {
		p.sinceChunk += step
	} else {
		p.sinceChunk = 0
	}

	obs := Observation{
		Concurrency:         ctx.Concurrency,
		ChunkSize:           ctx.ChunkSize,
		TotalBytes:          p.totalBytes,
		DoneBytes:           p.progressBytes(),
		AggregateThroughput: thr,
		RequestDuration:     ctx.RequestDuration,
		FailedRequests:      ctx.FailedRequests,
		Retries:             ctx.Retries,
		TotalAttempts:       ctx.TotalAttempts,
		ThroughputEWMA:      p.ewma,
		PrevThroughput:      p.prevAggregate,
		ThroughputVol:       p.vol,
		PrevDuration:        p.prevDuration,
		SinceConcChange:     p.sinceConc,
		SinceChunkChange:    p.sinceChunk,
		WindowFailures:      int64(countOnes(p.failWindow)),
		WindowRetries:       int64(countOnes(p.retryWindow)),
		WindowAttempts:      int64(len(p.failWindow)),
	}
	// Advance trackers for the next observation.
	p.vol = p.spec.EWMAAlpha*absFloat(thr-p.prevAggregate) + (1-p.spec.EWMAAlpha)*p.vol
	p.ewma = p.spec.EWMAAlpha*thr + (1-p.spec.EWMAAlpha)*p.ewma
	p.prevAggregate = thr
	p.prevDuration = ctx.RequestDuration
	p.prevFailures, p.prevRetries = ctx.FailedRequests, ctx.Retries
	p.prevSuccesses = ctx.SuccessfulRequests
	p.havePrev = true
	return obs
}

// observeV5 builds a v5 ObservationV5 from the downloader context and advances
// the shared temporal trackers. It must be called under p.mu.
func (p *LivePolicy) observeV5(ctx policy.Context) ObservationV5 {
	thr := ctx.AggregateThroughput

	failDelta := ctx.FailedRequests - p.prevFailures
	retryDelta := ctx.Retries - p.prevRetries
	completedDelta := ctx.SuccessfulRequests - p.v5Completed
	if completedDelta > 0 {
		p.doneBytesAccum += ctx.BytesTransferred
	}
	p.pushWindow(&p.failWindow, boolToInt(failDelta > 0))
	p.pushWindow(&p.retryWindow, boolToInt(retryDelta > 0))
	p.pushWindow(&p.completedWindow, int(completedDelta))

	step := ctx.RequestDuration
	if step <= 0 {
		step = time.Millisecond
	}
	if ctx.Concurrency == p.prevConc {
		p.sinceConc += step
	} else {
		p.sinceConc = 0
	}

	obs := ObservationV5{
		Concurrency:         ctx.Concurrency,
		TotalBytes:          p.totalBytes,
		DoneBytes:           p.progressBytes(),
		AggregateThroughput: thr,
		RequestDuration:     ctx.RequestDuration,
		ThroughputEWMA:      p.ewma,
		PrevThroughput:      p.prevAggregate,
		ThroughputVol:       p.vol,
		PrevDuration:        p.prevDuration,
		SinceConcChange:     p.sinceConc,
		WindowFailures:      int64(countOnes(p.failWindow)),
		WindowRetries:       int64(countOnes(p.retryWindow)),
		WindowAttempts:      int64(len(p.failWindow)),
		WindowCompleted:     int64(sumInts(p.completedWindow)),
	}
	p.vol = p.spec.EWMAAlpha*absFloat(thr-p.prevAggregate) + (1-p.spec.EWMAAlpha)*p.vol
	p.ewma = p.spec.EWMAAlpha*thr + (1-p.spec.EWMAAlpha)*p.ewma
	p.prevAggregate = thr
	p.prevDuration = ctx.RequestDuration
	p.prevFailures, p.prevRetries = ctx.FailedRequests, ctx.Retries
	p.v5Completed = ctx.SuccessfulRequests
	p.havePrev = true
	return obs
}

// recordV5 records a v5 decision for observability.
func (p *LivePolicy) recordV5(idx int, obs ObservationV5, ctx policy.Context) {
	x, _ := obs.FeaturesV5(p.spec)
	effective, noop := concurrencyEffect(idx, ctx.Concurrency, p.spec)
	p.log = append(p.log, Decision{
		Index:               len(p.log),
		Action:              p.actionName(idx),
		ActionIndex:         idx,
		ConcurrencyBefore:   ctx.Concurrency,
		ConcurrencyAfter:    ctx.Concurrency,
		ChunkBefore:         ctx.ChunkSize,
		ChunkAfter:          ctx.ChunkSize,
		AggregateThroughput: ctx.AggregateThroughput,
		RequestDuration:     ctx.RequestDuration,
		RemainingBytes:      obs.TotalBytes - obs.DoneBytes,
		Effective:           effective,
		NoOp:                noop,
		Features:            x,
	})
}

// recordV6 records a v6 decision for observability.
func (p *LivePolicy) recordV6(idx int, obs ObservationV6, ctx policy.Context) {
	x, _ := obs.FeaturesV6(p.spec)
	effective, noop := concurrencyEffect(idx, ctx.Concurrency, p.spec)
	p.log = append(p.log, Decision{
		Index:               len(p.log),
		Action:              p.actionName(idx),
		ActionIndex:         idx,
		ConcurrencyBefore:   ctx.Concurrency,
		ConcurrencyAfter:    ctx.Concurrency,
		ChunkBefore:         ctx.ChunkSize,
		ChunkAfter:          ctx.ChunkSize,
		AggregateThroughput: ctx.AggregateThroughput,
		RequestDuration:     ctx.RequestDuration,
		RemainingBytes:      obs.TotalBytes - obs.DoneBytes,
		Effective:           effective,
		NoOp:                noop,
		Features:            x,
	})
}

// concurrencyEffect reports whether a concurrency-only action index changes the
// concurrency given the current value and spec bounds.
func concurrencyEffect(idx, conc int, spec StateSpec) (effective, noop bool) {
	switch idx {
	case 0:
		return true, true
	case 1:
		effective = conc < spec.MaxConcurrency
	case 2:
		effective = conc > spec.MinConcurrency
	}
	if idx != 0 {
		noop = !effective
	}
	return effective, noop
}

func sumInts(v []int) int {
	s := 0
	for _, x := range v {
		s += x
	}
	return s
}

// progressBytes returns completed output bytes. The downloader calls
// SelectAction once per completed attempt with that attempt's BytesTransferred
// and cumulative SuccessfulRequests, so progress is the accumulated bytes of
// successful attempts (each success wrote exactly one completed range). A failed
// attempt never advances it.
func (p *LivePolicy) progressBytes() int64 {
	done := p.doneBytesAccum
	if p.totalBytes > 0 && done > p.totalBytes {
		done = p.totalBytes
	}
	return done
}

func (p *LivePolicy) record(idx int, obs Observation, ctx policy.Context) {
	x, _ := obs.Features(p.spec)
	effective := false
	noop := true
	switch idx {
	case 0:
		effective, noop = true, true
	case 1:
		effective = ctx.Concurrency < p.spec.MaxConcurrency
	case 2:
		effective = ctx.Concurrency > p.spec.MinConcurrency
	case 3:
		effective = ctx.ChunkSize+32768 <= p.spec.MaxChunkSize
	case 4:
		effective = ctx.ChunkSize-32768 >= p.spec.MinChunkSize
	}
	if idx != 0 {
		noop = !effective
	}
	p.log = append(p.log, Decision{
		Index:               len(p.log),
		Action:              p.actionName(idx),
		ActionIndex:         idx,
		ConcurrencyBefore:   ctx.Concurrency,
		ConcurrencyAfter:    ctx.Concurrency,
		ChunkBefore:         ctx.ChunkSize,
		ChunkAfter:          ctx.ChunkSize,
		AggregateThroughput: ctx.AggregateThroughput,
		RequestDuration:     ctx.RequestDuration,
		RemainingBytes:      obs.TotalBytes - obs.DoneBytes,
		Effective:           effective,
		NoOp:                noop,
		Features:            x,
	})
}

// Log returns a copy of the recorded decisions.
func (p *LivePolicy) Log() []Decision {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]Decision(nil), p.log...)
}

func (p *LivePolicy) pushWindow(w *[]int, v int) {
	*w = append(*w, v)
	if len(*w) > p.spec.CounterWindow {
		*w = (*w)[len(*w)-p.spec.CounterWindow:]
	}
}

func absFloat(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

var _ policy.Policy = (*LivePolicy)(nil)
