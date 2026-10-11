// Package diagnostics implements clearly-labeled, one-off diagnostic
// experiments. It is NOT part of the production downloader/experiment path and
// changes no production behavior. Its only purpose is measurement.
//
// The LinUCB exploration diagnostic forces each action deterministically so we
// can observe whether the environment produces distinguishable reward feedback
// for it, independently of the learner's own action selection.
package diagnostics

import (
	"context"
	"fmt"
	"math"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"adaptivedl/internal/downloader"
	"adaptivedl/internal/experiment/network"
	"adaptivedl/internal/experiment/server"
	"adaptivedl/internal/metrics"
	"adaptivedl/internal/policy"
	"adaptivedl/internal/rl"
)

// Base configuration, identical to the locked formal benchmark
// (DefaultMatrixConfig) so the diagnostic is comparable to it.
const (
	FileSize     = int64(256 << 10)
	ChunkSize    = int64(32 << 10)
	MinChunkSize = int64(32 << 10)
	MaxChunkSize = int64(128 << 10)
	InitChunk    = int64(32 << 10)
	ChunkStep    = int64(32 << 10)

	InitConcurrency = 1
	MinConcurrency  = 1
	MaxConcurrency  = 4

	PolicyWindow = 4
	MaxRetries   = 3
	Timeout      = 30 * time.Second
	LinUCBAlpha  = 0.2
)

// Scenario describes one forced-action scenario. It mirrors the formal
// benchmark scenarios, using application-level server knobs where possible;
// loss uses tc/netem because the server has no loss model.
type Scenario struct {
	Name            string
	InitialRate     int64
	ChangedRate     int64
	RateChangeAfter int64
	ServerDelay     time.Duration
	FailFirst       int
	LossPercent     float64
}

// FormalScenarios mirrors DefaultMatrixConfig's five scenarios.
func FormalScenarios() []Scenario {
	return []Scenario{
		{Name: "baseline", InitialRate: 4 << 20, ChangedRate: 4 << 20},
		{Name: "server-throttle", InitialRate: 512 << 10, ChangedRate: 4 << 20, RateChangeAfter: 4},
		{Name: "latency", InitialRate: 4 << 20, ChangedRate: 4 << 20, ServerDelay: 20 * time.Millisecond},
		{Name: "loss", InitialRate: 4 << 20, ChangedRate: 4 << 20, LossPercent: 1},
		{Name: "combined", InitialRate: 1 << 20, ChangedRate: 4 << 20, ServerDelay: 20 * time.Millisecond, LossPercent: 1},
	}
}

// forcePolicy implements policy.Policy and always returns one fixed action,
// recording every context it is asked about and every reward the runner
// attributes to it (for an integrity cross-check). It performs no learning.
type forcePolicy struct {
	action rl.ActionIndex

	mu      sync.Mutex
	ctxs    []policy.Context
	rewards []float64
}

func (p *forcePolicy) SelectAction(ctx policy.Context) policy.Action {
	p.mu.Lock()
	p.ctxs = append(p.ctxs, ctx)
	p.mu.Unlock()
	a, err := rl.ActionFromIndex(p.action)
	if err != nil {
		return policy.NoChange()
	}
	return a
}

func (p *forcePolicy) Update(_ policy.Context, _ policy.Action, reward float64) {
	p.mu.Lock()
	p.rewards = append(p.rewards, reward)
	p.mu.Unlock()
}

func (p *forcePolicy) contexts() []policy.Context {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]policy.Context(nil), p.ctxs...)
}

func (p *forcePolicy) updateRewards() []float64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]float64(nil), p.rewards...)
}

// RunResult is one forced-action download.
type RunResult struct {
	Scenario    string
	Action      string
	ActionIndex int
	Rep         int
	Netem       string // "none", "applied", "unavailable"

	Decisions int
	Err       string

	OutputMatches     bool
	NoDuplicateRanges bool
	NoMissingRanges   bool

	FinalConcurrency int
	FinalChunkSize   int64
	MaxConcurrency   int

	// ReconMismatches counts transitions where the reward reconstructed from
	// the recorded contexts differs from the reward the runner attributed to
	// the forced action. Non-zero would indicate a diagnostic/integrity bug.
	ReconMismatches int

	Records []Transition
}

// Transition is the reconstructed outcome attributed to one forced decision.
type Transition struct {
	Index int

	PrevConcurrency int
	CurConcurrency  int
	PrevChunkSize   int64
	CurChunkSize    int64

	PrevThroughput float64
	CurThroughput  float64
	PrevAggregate  float64
	CurAggregate   float64

	PrevDuration time.Duration
	CurDuration  time.Duration

	RetriesDelta int64
	FailedDelta  int64

	ControllerChanged bool
	Reward            float64
	Components        rl.RewardComponents
}

// ActionStats aggregates transitions across runs for one (scenario, action).
type ActionStats struct {
	Scenario    string
	Action      string
	ActionIndex int
	Netem       string
	Runs        int
	Decisions   int

	RewardMean     float64
	RewardMedian   float64
	RewardSD       float64
	RewardMin      float64
	RewardMax      float64
	Pos, Zero, Neg int

	Throughput  float64 // raw mean component
	Improvement float64
	Latency     float64
	Retry       float64
	Failure     float64
	ActionCost  float64

	WeightedThroughput  float64
	WeightedImprovement float64
	WeightedLatency     float64
	WeightedRetry       float64
	WeightedFailure     float64
	WeightedAction      float64

	DeltaThroughput   float64 // cur - prev (per-request)
	DeltaAggregate    float64 // cur - prev (aggregate)
	FirstReward       float64
	FirstDeltaAgg     float64
	ControllerChanged int
	OverlapDecisions  int // concurrency > 1 and aggregate > 1.2 * per-request

	FinalConcurrency float64
	FinalChunkSize   float64
	MaxConcurrency   int

	ReconMismatches int

	// accumulators (unexported; not serialized)
	rewards          []float64
	firstRewards     []float64
	sumThroughput    float64
	sumImprovement   float64
	sumLatency       float64
	sumRetry         float64
	sumFailure       float64
	sumAction        float64
	sumDeltaThrough  float64
	sumDeltaAgg      float64
	sumFirstDeltaAgg float64
}

// Report is the full diagnostic output.
type Report struct {
	Alpha        float64
	RewardConfig rl.RewardConfig
	Repetitions  int
	Scenarios    []ScenarioReport
}

// ScenarioReport groups the forced-action stats for one scenario.
type ScenarioReport struct {
	Name      string
	Netem     string
	Actions   []ActionStats
	InitState *InitState
}

// InitState is the first observed context of the scenario (concurrency 1).
type InitState struct {
	Concurrency         int
	ChunkSize           int64
	Throughput          float64
	AggregateThroughput float64
	RequestDuration     time.Duration
	BytesTransferred    int64
	FeatureNorm         float64
	InitialScore        float64 // alpha*||x||: identical for every action
}

// ForcedActions returns the five supported actions in canonical order.
func ForcedActions() []rl.ActionIndex {
	return []rl.ActionIndex{
		rl.ActionNone,
		rl.ActionIncreaseConcurrency,
		rl.ActionDecreaseConcurrency,
		rl.ActionIncreaseChunkSize,
		rl.ActionDecreaseChunkSize,
	}
}

// Config configures the diagnostic run.
type Config struct {
	Scenarios   []Scenario
	Repetitions int
	// NetworkController applies tc/netem for loss scenarios. Nil disables
	// netem (loss is then reported as unavailable).
	NetworkController *network.Controller
}

// Run executes the forced-action diagnostic.
func Run(cfg Config) (Report, error) {
	if cfg.Repetitions <= 0 {
		cfg.Repetitions = 5
	}
	rewardCfg := rl.DefaultRewardConfig()
	rep := Report{Alpha: LinUCBAlpha, RewardConfig: rewardCfg, Repetitions: cfg.Repetitions}

	stateCfg := stateConfig()
	calc, err := rl.NewRewardCalculator(rewardCfg)
	if err != nil {
		return Report{}, err
	}

	for _, sc := range cfg.Scenarios {
		sr := ScenarioReport{Name: sc.Name, Netem: "none"}
		byAction := map[rl.ActionIndex]*ActionStats{}
		var initState *InitState

		for _, action := range ForcedActions() {
			stats := &ActionStats{Scenario: sc.Name, Action: action.String(), ActionIndex: int(action)}
			for r := 0; r < cfg.Repetitions; r++ {
				rr, err := runForced(sc, action, r, cfg.NetworkController, calc)
				if err != nil {
					return Report{}, fmt.Errorf("scenario %s action %s rep %d: %w", sc.Name, action, r, err)
				}
				stats.Runs++
				stats.Netem = rr.Netem
				switch rr.Netem {
				case "applied":
					sr.Netem = "applied"
				case "unavailable":
					if sr.Netem != "applied" {
						sr.Netem = "unavailable"
					}
				}
				stats.FinalConcurrency += float64(rr.FinalConcurrency)
				stats.FinalChunkSize += float64(rr.FinalChunkSize)
				stats.ReconMismatches += rr.ReconMismatches
				if rr.MaxConcurrency > stats.MaxConcurrency {
					stats.MaxConcurrency = rr.MaxConcurrency
				}
				for _, tr := range rr.Records {
					stats.add(tr)
				}
				if initState == nil && len(rr.Records) > 0 && action == rl.ActionNone {
					initState = buildInitState(rr.Records[0], stateCfg)
				}
			}
			stats.finish()
			byAction[action] = stats
		}
		for _, action := range ForcedActions() {
			sr.Actions = append(sr.Actions, *byAction[action])
		}
		sr.InitState = initState
		rep.Scenarios = append(rep.Scenarios, sr)
	}
	return rep, nil
}

func stateConfig() rl.Config {
	return rl.Config{
		MinConcurrency: MinConcurrency, MaxConcurrency: MaxConcurrency,
		MinChunkSize: 1, MaxChunkSize: MaxChunkSize,
		MaxThroughput: 1e9, MaxRequestDuration: 1 << 40, CounterScale: 1000,
	}
}

func (s *ActionStats) add(tr Transition) {
	s.Decisions++
	s.rewards = append(s.rewards, tr.Reward)
	c := tr.Components
	s.sumThroughput += c.Throughput
	s.sumImprovement += c.ThroughputImprovement
	s.sumLatency += c.LatencyCost
	s.sumRetry += c.RetryCost
	s.sumFailure += c.FailureCost
	s.sumAction += c.ActionCost
	s.sumDeltaThrough += tr.CurThroughput - tr.PrevThroughput
	s.sumDeltaAgg += tr.CurAggregate - tr.PrevAggregate
	if tr.ControllerChanged {
		s.ControllerChanged++
	}
	if tr.CurConcurrency > 1 && tr.CurThroughput > 0 && tr.CurAggregate > 1.2*tr.CurThroughput {
		s.OverlapDecisions++
	}
	if tr.Index == 0 {
		s.firstRewards = append(s.firstRewards, tr.Reward)
		s.sumFirstDeltaAgg += tr.CurAggregate - tr.PrevAggregate
	}
}

func (s *ActionStats) finish() {
	if n := float64(s.Decisions); n > 0 {
		s.Throughput = s.sumThroughput / n
		s.Improvement = s.sumImprovement / n
		s.Latency = s.sumLatency / n
		s.Retry = s.sumRetry / n
		s.Failure = s.sumFailure / n
		s.ActionCost = s.sumAction / n
		s.WeightedThroughput = s.Throughput
		s.WeightedImprovement = s.Improvement
		s.WeightedLatency = 0.5 * s.Latency
		s.WeightedRetry = 0.5 * s.Retry
		s.WeightedFailure = s.Failure
		s.WeightedAction = s.ActionCost
		s.DeltaThroughput = s.sumDeltaThrough / n
		s.DeltaAggregate = s.sumDeltaAgg / n
	}
	if len(s.rewards) > 0 {
		s.RewardMean = mean(s.rewards)
		s.RewardMedian = median(s.rewards)
		s.RewardSD = stdev(s.rewards)
		s.RewardMin, s.RewardMax = minMax(s.rewards)
		for _, v := range s.rewards {
			switch {
			case v > 0:
				s.Pos++
			case v < 0:
				s.Neg++
			default:
				s.Zero++
			}
		}
	}
	if len(s.firstRewards) > 0 {
		s.FirstReward = mean(s.firstRewards)
		s.FirstDeltaAgg = s.sumFirstDeltaAgg / float64(len(s.firstRewards))
	}
	if s.Runs > 0 {
		s.FinalConcurrency /= float64(s.Runs)
		s.FinalChunkSize /= float64(s.Runs)
	}
}

func buildInitState(tr Transition, cfg rl.Config) *InitState {
	st := rl.State{
		Concurrency:         tr.PrevConcurrency,
		ChunkSize:           tr.PrevChunkSize,
		Throughput:          tr.PrevThroughput,
		AggregateThroughput: tr.PrevAggregate,
		RequestDuration:     tr.PrevDuration,
		BytesTransferred:    32768,
		SuccessfulRequests:  1,
		TotalAttempts:       1,
	}
	x, err := st.Features(cfg)
	if err != nil {
		return nil
	}
	var norm float64
	for _, v := range x {
		norm += v * v
	}
	norm = math.Sqrt(norm)
	return &InitState{
		Concurrency:         st.Concurrency,
		ChunkSize:           st.ChunkSize,
		Throughput:          st.Throughput,
		AggregateThroughput: st.AggregateThroughput,
		RequestDuration:     st.RequestDuration,
		BytesTransferred:    st.BytesTransferred,
		FeatureNorm:         norm,
		InitialScore:        LinUCBAlpha * norm,
	}
}

// runForced performs one forced-action download and reconstructs the rewards
// with the production RewardCalculator, exactly as policyRunner does.
func runForced(sc Scenario, action rl.ActionIndex, rep int, nc *network.Controller, calc *rl.RewardCalculator) (RunResult, error) {
	res := RunResult{Scenario: sc.Name, Action: action.String(), ActionIndex: int(action), Rep: rep, Netem: "none"}

	content := contentPattern(FileSize)
	srv := server.New(content)
	srv.SetRate(sc.InitialRate)
	if sc.RateChangeAfter > 0 {
		srv.SetRateAfter(sc.RateChangeAfter, sc.ChangedRate)
	}
	if sc.ServerDelay > 0 {
		srv.SetDelay(sc.ServerDelay)
	}
	if sc.FailFirst > 0 {
		srv.FailNext(sc.FailFirst)
	}
	ts := httptest.NewServer(srv)
	defer ts.Close()

	dir, err := os.MkdirTemp("", "adaptivedl-diag-")
	if err != nil {
		return res, err
	}
	defer os.RemoveAll(dir)
	outPath := filepath.Join(dir, "out.bin")

	if sc.LossPercent > 0 {
		if nc == nil {
			res.Netem = "unavailable"
			res.Err = "loss scenario requires tc/netem (not configured)"
			return res, nil
		}
		if err := nc.Apply("lo", network.Config{LossPercent: sc.LossPercent}); err != nil {
			res.Netem = "unavailable"
			res.Err = "netem apply: " + err.Error()
			return res, nil
		}
		defer func() { _ = nc.Clear("lo") }()
		res.Netem = "applied"
	}

	fp := &forcePolicy{action: action}
	rewardCfg := rl.DefaultRewardConfig()
	dl, err := downloader.New(downloader.Config{
		URL:              ts.URL,
		OutputPath:       outPath,
		TotalSize:        FileSize,
		Concurrency:      InitConcurrency,
		ChunkSize:        ChunkSize,
		Timeout:          Timeout,
		MaxRetries:       MaxRetries,
		MinConcurrency:   MinConcurrency,
		MaxConcurrency:   MaxConcurrency,
		MinChunkSize:     MinChunkSize,
		MaxChunkSize:     MaxChunkSize,
		InitialChunkSize: InitChunk,
		ChunkSizeStep:    ChunkStep,
		PolicyWindowSize: PolicyWindow,
		Policy:           fp,
		RewardConfig:     &rewardCfg,
	})
	if err != nil {
		return res, err
	}
	if runErr := dl.Download(context.Background()); runErr != nil {
		res.Err = runErr.Error()
	}

	snap := dl.Metrics()
	res.OutputMatches, res.NoDuplicateRanges, res.NoMissingRanges = verifyDownload(content, outPath, snap.Records, FileSize)
	res.FinalConcurrency = dl.Concurrency()
	res.FinalChunkSize = dl.ChunkSize()

	ctxs := fp.contexts()
	updates := fp.updateRewards()
	res.Decisions = len(ctxs)
	for i := 0; i+1 < len(ctxs); i++ {
		reward := calc.Calculate(stateOf(ctxs[i]), stateOf(ctxs[i+1]), action, true)
		if i < len(updates) && math.Abs(reward.Value-updates[i]) > 1e-9 {
			res.ReconMismatches++
		}
		tr := Transition{
			Index:             i,
			PrevConcurrency:   ctxs[i].Concurrency,
			CurConcurrency:    ctxs[i+1].Concurrency,
			PrevChunkSize:     ctxs[i].ChunkSize,
			CurChunkSize:      ctxs[i+1].ChunkSize,
			PrevThroughput:    ctxs[i].Throughput,
			CurThroughput:     ctxs[i+1].Throughput,
			PrevAggregate:     ctxs[i].AggregateThroughput,
			CurAggregate:      ctxs[i+1].AggregateThroughput,
			PrevDuration:      ctxs[i].RequestDuration,
			CurDuration:       ctxs[i+1].RequestDuration,
			RetriesDelta:      ctxs[i+1].Retries - ctxs[i].Retries,
			FailedDelta:       ctxs[i+1].FailedRequests - ctxs[i].FailedRequests,
			ControllerChanged: controllerChanged(action, ctxs[i], ctxs[i+1]),
			Reward:            reward.Value,
			Components:        reward.Components,
		}
		if tr.CurConcurrency > res.MaxConcurrency {
			res.MaxConcurrency = tr.CurConcurrency
		}
		res.Records = append(res.Records, tr)
	}
	return res, nil
}

func stateOf(ctx policy.Context) rl.State {
	return rl.State{
		Concurrency:         ctx.Concurrency,
		ChunkSize:           ctx.ChunkSize,
		Throughput:          ctx.Throughput,
		AggregateThroughput: ctx.AggregateThroughput,
		RequestDuration:     ctx.RequestDuration,
		BytesTransferred:    ctx.BytesTransferred,
		SuccessfulRequests:  ctx.SuccessfulRequests,
		FailedRequests:      ctx.FailedRequests,
		Retries:             ctx.Retries,
		TotalAttempts:       ctx.TotalAttempts,
	}
}

func controllerChanged(action rl.ActionIndex, prev, cur policy.Context) bool {
	switch action {
	case rl.ActionIncreaseConcurrency, rl.ActionDecreaseConcurrency:
		return cur.Concurrency != prev.Concurrency
	case rl.ActionIncreaseChunkSize, rl.ActionDecreaseChunkSize:
		return cur.ChunkSize != prev.ChunkSize
	default:
		return false
	}
}

func contentPattern(n int64) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte((int64(i)*31 + 7) % 256)
	}
	return b
}

func verifyDownload(content []byte, outPath string, records []metrics.Record, size int64) (match, noDup, noMissing bool) {
	got, err := os.ReadFile(outPath)
	if err != nil {
		return false, false, false
	}
	match = string(got) == string(content)
	type seg struct{ s, e int64 }
	segs := make([]seg, 0, len(records))
	for _, r := range records {
		if r.Success {
			segs = append(segs, seg{r.RangeStart, r.RangeEnd})
		}
	}
	sort.Slice(segs, func(i, j int) bool { return segs[i].s < segs[j].s })
	noDup, noMissing = true, true
	var prevEnd int64 = -1
	for _, s := range segs {
		if s.s <= prevEnd {
			noDup = false
		}
		if s.s != prevEnd+1 {
			noMissing = false
		}
		if s.e > prevEnd {
			prevEnd = s.e
		}
	}
	if prevEnd != size-1 {
		noMissing = false
	}
	return
}

func mean(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	var s float64
	for _, x := range v {
		s += x
	}
	return s / float64(len(v))
}

func median(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	c := append([]float64(nil), v...)
	sort.Float64s(c)
	n := len(c)
	if n%2 == 1 {
		return c[n/2]
	}
	return (c[n/2-1] + c[n/2]) / 2
}

func stdev(v []float64) float64 {
	if len(v) < 2 {
		return 0
	}
	m := mean(v)
	var s float64
	for _, x := range v {
		s += (x - m) * (x - m)
	}
	return math.Sqrt(s / float64(len(v)))
}

func minMax(v []float64) (float64, float64) {
	if len(v) == 0 {
		return 0, 0
	}
	lo, hi := v[0], v[0]
	for _, x := range v {
		if x < lo {
			lo = x
		}
		if x > hi {
			hi = x
		}
	}
	return lo, hi
}
