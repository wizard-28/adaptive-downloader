package experiment

import (
	"fmt"
	"math"
	"sort"
	"time"

	"adaptivedl/internal/experiment/network"
	"adaptivedl/internal/rl"
)

// This file implements a small, deterministic LinUCB calibration and
// reward-diagnostics layer. It is DESCRIPTIVE ONLY: it measures reward and
// learner behaviour and never ranks modes, tunes weights, or claims
// performance. It reuses the existing experiment framework (RunMatrix,
// RunResult, DecisionEvent) and the existing RewardCalculator; it does not
// introduce a second framework.

// rewardReconstructionTolerance is the maximum absolute difference allowed
// between the scalar reward the runner attributed to a decision and the reward
// reconstructed from the recorded observations. A larger difference indicates a
// bug in reward computation or credit assignment, not a tuning issue.
const rewardReconstructionTolerance = 1e-9

// ActionDistribution counts how often each RL action was selected.
type ActionDistribution struct {
	// Counts is indexed by rl.ActionIndex.
	Counts [rl.NumActions]int
	Total  int
}

// Count returns how many times action a was selected.
func (d ActionDistribution) Count(a rl.ActionIndex) int {
	if int(a) < 0 || int(a) >= rl.NumActions {
		return 0
	}
	return d.Counts[a]
}

// Fraction returns the share of decisions that selected action a.
func (d ActionDistribution) Fraction(a rl.ActionIndex) float64 {
	if d.Total == 0 {
		return 0
	}
	return float64(d.Count(a)) / float64(d.Total)
}

// Unique returns the number of distinct actions selected.
func (d ActionDistribution) Unique() int {
	n := 0
	for _, c := range d.Counts {
		if c > 0 {
			n++
		}
	}
	return n
}

// ExploredConcurrency reports whether any concurrency action was selected.
func (d ActionDistribution) ExploredConcurrency() bool {
	return d.Count(rl.ActionIncreaseConcurrency)+d.Count(rl.ActionDecreaseConcurrency) > 0
}

// ExploredChunkSize reports whether any chunk-size action was selected.
func (d ActionDistribution) ExploredChunkSize() bool {
	return d.Count(rl.ActionIncreaseChunkSize)+d.Count(rl.ActionDecreaseChunkSize) > 0
}

// DistributionFromActions counts a sequence of RL actions.
func DistributionFromActions(actions []rl.ActionIndex) ActionDistribution {
	var d ActionDistribution
	for _, a := range actions {
		if int(a) < 0 || int(a) >= rl.NumActions {
			continue
		}
		d.Counts[a]++
		d.Total++
	}
	return d
}

// RewardStats summarizes a sequence of scalar rewards.
type RewardStats struct {
	Count                          int
	Mean, Median, Min, Max, StdDev float64
	Positive, Zero, Negative       int
}

// RewardStatsOf computes descriptive statistics over reward values.
func RewardStatsOf(values []float64) RewardStats {
	s := RewardStats{Count: len(values)}
	if len(values) == 0 {
		return s
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	s.Mean = meanFloat(values)
	s.Median = medianFloat(sorted)
	s.Min = sorted[0]
	s.Max = sorted[len(sorted)-1]
	s.StdDev = stddevFloat(values)
	for _, v := range values {
		switch {
		case v > 0:
			s.Positive++
		case v < 0:
			s.Negative++
		default:
			s.Zero++
		}
	}
	return s
}

// ComponentStats summarizes the mean raw and weighted contribution of each
// reward component. Raw means are the average component values; weighted means
// apply the configured weights so relative magnitudes are directly comparable.
type ComponentStats struct {
	Count int

	MeanThroughput            float64
	MeanThroughputImprovement float64
	MeanLatencyCost           float64
	MeanRetryCost             float64
	MeanFailureCost           float64
	MeanActionCost            float64

	MeanWeightedThroughput            float64
	MeanWeightedThroughputImprovement float64
	MeanWeightedLatencyCost           float64
	MeanWeightedRetryCost             float64
	MeanWeightedFailureCost           float64
	MeanWeightedActionCost            float64
}

// ComponentStatsOf aggregates reward components and applies cfg's weights.
func ComponentStatsOf(components []rl.RewardComponents, cfg rl.RewardConfig) ComponentStats {
	s := ComponentStats{Count: len(components)}
	if len(components) == 0 {
		return s
	}
	var t, i, l, r, f, a float64
	for _, c := range components {
		t += c.Throughput
		i += c.ThroughputImprovement
		l += c.LatencyCost
		r += c.RetryCost
		f += c.FailureCost
		a += c.ActionCost
	}
	n := float64(len(components))
	s.MeanThroughput = t / n
	s.MeanThroughputImprovement = i / n
	s.MeanLatencyCost = l / n
	s.MeanRetryCost = r / n
	s.MeanFailureCost = f / n
	s.MeanActionCost = a / n

	s.MeanWeightedThroughput = cfg.WThroughput * s.MeanThroughput
	s.MeanWeightedThroughputImprovement = cfg.WImprovement * s.MeanThroughputImprovement
	s.MeanWeightedLatencyCost = cfg.WLatency * s.MeanLatencyCost
	s.MeanWeightedRetryCost = cfg.WRetry * s.MeanRetryCost
	s.MeanWeightedFailureCost = cfg.WFailure * s.MeanFailureCost
	s.MeanWeightedActionCost = cfg.WAction * s.MeanActionCost
	return s
}

// ChurnStats describes action and controller churn.
type ChurnStats struct {
	Decisions          int
	ActionTransitions  int
	ChangesPerDecision float64
	LongestSameRun     int
	ConcurrencyChanges int
	ChunkSizeChanges   int
}

// ActionChurn returns the number of adjacent action transitions and the longest
// run of identical consecutive actions.
func ActionChurn(actions []rl.ActionIndex) (transitions, longestRun int) {
	if len(actions) == 0 {
		return 0, 0
	}
	longestRun = 1
	run := 1
	for i := 1; i < len(actions); i++ {
		if actions[i] != actions[i-1] {
			transitions++
			run = 1
			continue
		}
		run++
		if run > longestRun {
			longestRun = run
		}
	}
	return transitions, longestRun
}

// ChurnFromDecisions computes churn from a decision sequence, using the actual
// controller targets (before vs after each action) for concurrency/chunk churn.
func ChurnFromDecisions(decs []DecisionEvent) ChurnStats {
	s := ChurnStats{Decisions: len(decs)}
	actions := make([]rl.ActionIndex, 0, len(decs))
	for _, d := range decs {
		if d.ActionIndex >= 0 {
			actions = append(actions, rl.ActionIndex(d.ActionIndex))
		}
	}
	s.ActionTransitions, s.LongestSameRun = ActionChurn(actions)
	if s.Decisions > 0 {
		s.ChangesPerDecision = float64(s.ActionTransitions) / float64(s.Decisions)
	}
	for _, d := range decs {
		if d.ConcurrencyAfter != d.Concurrency {
			s.ConcurrencyChanges++
		}
		if d.ChunkSizeAfter != d.ChunkSize {
			s.ChunkSizeChanges++
		}
	}
	return s
}

// RunDiagnostics is the descriptive diagnostic record for one LinUCB run.
type RunDiagnostics struct {
	Mode       Mode
	Scenario   string
	Repetition int
	Valid      bool

	Decisions int

	Actions    ActionDistribution
	Rewards    RewardStats
	Components ComponentStats
	Churn      ChurnStats

	EarlyActions ActionDistribution
	LateActions  ActionDistribution

	CumulativeReward float64

	// ReconstructionMismatches counts decisions where the reconstructed reward
	// did not match the reward the runner attributed to the action. Any
	// non-zero value indicates a reward/credit-assignment bug.
	ReconstructionMismatches int
	NumericallyHealthy       bool

	// Pooled across runs; unexported so they are not serialized.
	rewardValues     []float64
	rewardComponents []rl.RewardComponents
}

func stateOf(d DecisionEvent) rl.State {
	// Concurrency and ChunkSize are included because ActionCost now depends on
	// whether an action actually changed the controller target; the calibration
	// reconstruction must feed the same fields the runner does. The throughput
	// fields include AggregateThroughput because the reward's throughput
	// components are computed from it.
	return rl.State{
		Concurrency:         d.Concurrency,
		ChunkSize:           d.ChunkSize,
		Throughput:          d.Throughput,
		AggregateThroughput: d.AggregateThroughput,
		RequestDuration:     d.RequestDuration,
		Retries:             d.Retries,
		FailedRequests:      d.FailedRequests,
	}
}

func actionIndices(decs []DecisionEvent) []rl.ActionIndex {
	out := make([]rl.ActionIndex, 0, len(decs))
	for _, d := range decs {
		if d.ActionIndex >= 0 {
			out = append(out, rl.ActionIndex(d.ActionIndex))
		}
	}
	return out
}

// DiagnoseRun computes diagnostics for one LinUCB run. It reconstructs each
// reward from the recorded observations using the existing RewardCalculator and
// cross-checks it against the reward the runner attributed to the action; a
// mismatch is reported and marks the run numerically unhealthy.
func DiagnoseRun(run RunResult, rewardCfg rl.RewardConfig) (RunDiagnostics, error) {
	calc, err := rl.NewRewardCalculator(rewardCfg)
	if err != nil {
		return RunDiagnostics{}, err
	}

	d := RunDiagnostics{
		Mode:       run.Mode,
		Scenario:   run.Scenario,
		Repetition: run.Repetition,
		Valid:      run.Err == "" && run.OutputMatches && run.NoDuplicateRanges && run.NoMissingRanges,
		Decisions:  len(run.Decisions),
	}
	d.Actions = DistributionFromActions(actionIndices(run.Decisions))
	half := len(run.Decisions) / 2
	d.EarlyActions = DistributionFromActions(actionIndices(run.Decisions[:half]))
	d.LateActions = DistributionFromActions(actionIndices(run.Decisions[half:]))
	d.Churn = ChurnFromDecisions(run.Decisions)

	healthy := true
	mismatches := 0
	var values []float64
	var comps []rl.RewardComponents
	for i := 0; i+1 < len(run.Decisions); i++ {
		if run.Decisions[i].ActionIndex < 0 {
			continue
		}
		prev := stateOf(run.Decisions[i])
		cur := stateOf(run.Decisions[i+1])
		reward := calc.Calculate(prev, cur, rl.ActionIndex(run.Decisions[i].ActionIndex), true)
		if err := reward.Validate(); err != nil {
			healthy = false
			continue
		}
		if math.Abs(reward.Value-run.Decisions[i].Reward) > rewardReconstructionTolerance {
			mismatches++
		}
		recorded := run.Decisions[i].Reward
		if math.IsNaN(recorded) || math.IsInf(recorded, 0) {
			healthy = false
		}
		values = append(values, recorded)
		comps = append(comps, reward.Components)
		d.CumulativeReward += recorded
	}

	d.Rewards = RewardStatsOf(values)
	d.Components = ComponentStatsOf(comps, rewardCfg)
	d.ReconstructionMismatches = mismatches
	if mismatches > 0 {
		healthy = false
	}
	d.NumericallyHealthy = healthy
	d.rewardValues = values
	d.rewardComponents = comps
	return d, nil
}

// poolRuns pools per-run diagnostics into a scenario-level summary. Action and
// early/late counts are summed, churn counts are summed, and reward/component
// statistics are recomputed over the pooled values.
func poolRuns(runs []RunDiagnostics, rewardCfg rl.RewardConfig) RunDiagnostics {
	var p RunDiagnostics
	p.Mode = ModeLinUCB
	p.Scenario = "pooled"
	p.Valid = len(runs) > 0
	p.NumericallyHealthy = len(runs) > 0

	var values []float64
	var comps []rl.RewardComponents
	for _, r := range runs {
		for i := range p.Actions.Counts {
			p.Actions.Counts[i] += r.Actions.Counts[i]
			p.EarlyActions.Counts[i] += r.EarlyActions.Counts[i]
			p.LateActions.Counts[i] += r.LateActions.Counts[i]
		}
		p.Actions.Total += r.Actions.Total
		p.EarlyActions.Total += r.EarlyActions.Total
		p.LateActions.Total += r.LateActions.Total

		p.Churn.Decisions += r.Churn.Decisions
		p.Churn.ActionTransitions += r.Churn.ActionTransitions
		p.Churn.ConcurrencyChanges += r.Churn.ConcurrencyChanges
		p.Churn.ChunkSizeChanges += r.Churn.ChunkSizeChanges
		if r.Churn.LongestSameRun > p.Churn.LongestSameRun {
			p.Churn.LongestSameRun = r.Churn.LongestSameRun
		}

		values = append(values, r.rewardValues...)
		comps = append(comps, r.rewardComponents...)
		p.CumulativeReward += r.CumulativeReward
		p.ReconstructionMismatches += r.ReconstructionMismatches
		p.Decisions += r.Decisions
		if !r.Valid {
			p.Valid = false
		}
		if !r.NumericallyHealthy {
			p.NumericallyHealthy = false
		}
	}
	if p.Churn.Decisions > 0 {
		p.Churn.ChangesPerDecision = float64(p.Churn.ActionTransitions) / float64(p.Churn.Decisions)
	}
	p.Rewards = RewardStatsOf(values)
	p.Components = ComponentStatsOf(comps, rewardCfg)
	p.rewardValues = values
	p.rewardComponents = comps
	return p
}

// CalibrationConfig configures a calibration run.
type CalibrationConfig struct {
	Scenarios    []MatrixScenario
	Repetitions  int
	Alpha        float64
	RewardConfig rl.RewardConfig
	// NetworkController is passed through to RunMatrix. The calibration
	// scenarios use deterministic server knobs (delay/fail-next), not tc/netem,
	// so this is normally unused.
	NetworkController *network.Controller
}

// ScenarioCalibration holds the per-run diagnostics and pooled summary for one
// calibration scenario.
type ScenarioCalibration struct {
	Name   string
	Config ScenarioConfig
	Runs   []RunDiagnostics
	Pooled RunDiagnostics
}

// CalibrationReport is the full calibration output.
type CalibrationReport struct {
	Alpha        float64
	RewardConfig rl.RewardConfig
	Scenarios    []ScenarioCalibration
}

// RunCalibration runs the calibration scenarios in LinUCB mode and produces a
// descriptive report. It never ranks modes or tunes the reward.
func RunCalibration(cc CalibrationConfig) (CalibrationReport, error) {
	if err := cc.RewardConfig.Validate(); err != nil {
		return CalibrationReport{}, fmt.Errorf("calibration reward config: %w", err)
	}
	reps := cc.Repetitions
	if reps <= 0 {
		reps = 1
	}

	mscenarios := make([]MatrixScenario, len(cc.Scenarios))
	for i, cs := range cc.Scenarios {
		cfg := cs.Config
		cfg.LinUCBAlpha = cc.Alpha
		rw := cc.RewardConfig
		cfg.RewardConfig = &rw
		mscenarios[i] = MatrixScenario{Name: cs.Name, Config: cfg}
	}

	mr, err := RunMatrix(MatrixConfig{
		Scenarios:         mscenarios,
		Repetitions:       reps,
		Modes:             []Mode{ModeLinUCB},
		NetworkController: cc.NetworkController,
	})
	if err != nil {
		return CalibrationReport{}, err
	}

	report := CalibrationReport{Alpha: cc.Alpha, RewardConfig: cc.RewardConfig}
	for _, sr := range mr.Scenarios {
		sc := ScenarioCalibration{Name: sr.Name, Config: sr.Config}
		for _, run := range sr.LinUCB {
			rd, derr := DiagnoseRun(run, cc.RewardConfig)
			if derr != nil {
				return CalibrationReport{}, fmt.Errorf("scenario %q: %w", sr.Name, derr)
			}
			sc.Runs = append(sc.Runs, rd)
		}
		sc.Pooled = poolRuns(sc.Runs, cc.RewardConfig)
		report.Scenarios = append(report.Scenarios, sc)
	}
	return report, nil
}

// CalibrationScenarios returns the small deterministic calibration scenarios
// (A stable, B throttle, C latency, D failure, E combined). They use the
// existing experiment server's rate transition, deterministic delay, and
// deterministic fail-next mechanisms; no tc/netem is required.
func CalibrationScenarios() []MatrixScenario {
	base := func() ScenarioConfig {
		cfg := DefaultScenarioConfig()
		cfg.FileSize = 128 << 10
		cfg.ChunkSize = 8 << 10
		cfg.InitialConcurrency = 1
		cfg.MinConcurrency = 1
		cfg.MaxConcurrency = 3
		cfg.InitialRate = 512 << 10
		cfg.ChangedRate = 512 << 10
		cfg.RateChangeAfterRequests = 0
		cfg.MinChunkSize = 4 << 10
		cfg.MaxChunkSize = 32 << 10
		cfg.InitialChunkSize = 8 << 10
		cfg.ChunkSizeStep = 4 << 10
		cfg.ImprovementThreshold = 0.2
		cfg.DegradationThreshold = 0.2
		cfg.PolicyWindowSize = 4
		cfg.MaxRetries = 3
		cfg.Timeout = 10 * time.Second
		cfg.Repetitions = 1
		cfg.LinUCBAlpha = 0.2
		rw := rl.DefaultRewardConfig()
		cfg.RewardConfig = &rw
		return cfg
	}

	stable := base()

	throttle := base()
	throttle.InitialRate = 256 << 10
	throttle.ChangedRate = 2 << 20
	throttle.RateChangeAfterRequests = 4

	latency := base()
	latency.ServerDelay = 20 * time.Millisecond

	failure := base()
	failure.ServerFailFirstRequests = 3

	combined := base()
	combined.InitialRate = 256 << 10
	combined.ChangedRate = 2 << 20
	combined.RateChangeAfterRequests = 4
	combined.ServerDelay = 10 * time.Millisecond
	combined.ServerFailFirstRequests = 2

	return []MatrixScenario{
		{Name: "calibration-a-stable", Config: stable},
		{Name: "calibration-b-throttle", Config: throttle},
		{Name: "calibration-c-latency", Config: latency},
		{Name: "calibration-d-failure", Config: failure},
		{Name: "calibration-e-combined", Config: combined},
	}
}
