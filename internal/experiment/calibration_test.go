package experiment

import (
	"math"
	"reflect"
	"testing"
	"time"

	"adaptivedl/internal/policy"
	"adaptivedl/internal/rl"
)

func TestActionDistribution(t *testing.T) {
	// A B A A C, mapped onto the RL action space.
	actions := []rl.ActionIndex{
		rl.ActionNone,
		rl.ActionIncreaseConcurrency,
		rl.ActionNone,
		rl.ActionNone,
		rl.ActionDecreaseConcurrency,
	}
	d := DistributionFromActions(actions)
	if got := d.Count(rl.ActionNone); got != 3 {
		t.Fatalf("ActionNone count = %d, want 3", got)
	}
	if got := d.Count(rl.ActionIncreaseConcurrency); got != 1 {
		t.Fatalf("IncreaseConcurrency count = %d, want 1", got)
	}
	if got := d.Count(rl.ActionDecreaseConcurrency); got != 1 {
		t.Fatalf("DecreaseConcurrency count = %d, want 1", got)
	}
	if d.Total != 5 {
		t.Fatalf("Total = %d, want 5", d.Total)
	}
	if d.Unique() != 3 {
		t.Fatalf("Unique = %d, want 3", d.Unique())
	}
	if d.ExploredConcurrency() != true || d.ExploredChunkSize() != false {
		t.Fatalf("exploration flags = conc %v chunk %v, want true/false", d.ExploredConcurrency(), d.ExploredChunkSize())
	}
	if got := d.Fraction(rl.ActionNone); math.Abs(got-0.6) > 1e-12 {
		t.Fatalf("ActionNone fraction = %v, want 0.6", got)
	}
}

func TestRewardStatsOf(t *testing.T) {
	s := RewardStatsOf([]float64{1, -1, 0, 2})
	if s.Count != 4 {
		t.Fatalf("Count = %d, want 4", s.Count)
	}
	if math.Abs(s.Mean-0.5) > 1e-12 {
		t.Fatalf("Mean = %v, want 0.5", s.Mean)
	}
	if math.Abs(s.Median-0.5) > 1e-12 {
		t.Fatalf("Median = %v, want 0.5", s.Median)
	}
	if s.Min != -1 || s.Max != 2 {
		t.Fatalf("Min/Max = %v/%v, want -1/2", s.Min, s.Max)
	}
	if s.Positive != 2 || s.Zero != 1 || s.Negative != 1 {
		t.Fatalf("pos/zero/neg = %d/%d/%d, want 2/1/1", s.Positive, s.Zero, s.Negative)
	}
	wantStd := math.Sqrt(1.25)
	if math.Abs(s.StdDev-wantStd) > 1e-12 {
		t.Fatalf("StdDev = %v, want %v", s.StdDev, wantStd)
	}

	if empty := RewardStatsOf(nil); empty.Count != 0 {
		t.Fatalf("empty stats Count = %d, want 0", empty.Count)
	}
}

func TestActionChurn(t *testing.T) {
	// A A B B A -> two transitions, longest run 2.
	actions := []rl.ActionIndex{
		rl.ActionNone, rl.ActionNone,
		rl.ActionIncreaseConcurrency, rl.ActionIncreaseConcurrency,
		rl.ActionNone,
	}
	transitions, longest := ActionChurn(actions)
	if transitions != 2 {
		t.Fatalf("transitions = %d, want 2", transitions)
	}
	if longest != 2 {
		t.Fatalf("longest run = %d, want 2", longest)
	}

	if tr, lr := ActionChurn(nil); tr != 0 || lr != 0 {
		t.Fatalf("empty churn = %d/%d, want 0/0", tr, lr)
	}
	if tr, lr := ActionChurn([]rl.ActionIndex{rl.ActionNone}); tr != 0 || lr != 1 {
		t.Fatalf("single churn = %d/%d, want 0/1", tr, lr)
	}
}

func TestComponentStatsOf(t *testing.T) {
	cfg := rl.DefaultRewardConfig()
	comps := []rl.RewardComponents{
		{Throughput: 1, ThroughputImprovement: 1, LatencyCost: 0.5, RetryCost: 0, FailureCost: 0, ActionCost: 0.05},
		{Throughput: 2, ThroughputImprovement: 0, LatencyCost: 0.5, RetryCost: 0.5, FailureCost: 1, ActionCost: 0.05},
	}
	s := ComponentStatsOf(comps, cfg)
	if s.Count != 2 {
		t.Fatalf("Count = %d, want 2", s.Count)
	}
	if math.Abs(s.MeanThroughput-1.5) > 1e-12 {
		t.Fatalf("MeanThroughput = %v, want 1.5", s.MeanThroughput)
	}
	if math.Abs(s.MeanThroughputImprovement-0.5) > 1e-12 {
		t.Fatalf("MeanThroughputImprovement = %v, want 0.5", s.MeanThroughputImprovement)
	}
	if math.Abs(s.MeanLatencyCost-0.5) > 1e-12 {
		t.Fatalf("MeanLatencyCost = %v, want 0.5", s.MeanLatencyCost)
	}
	if math.Abs(s.MeanRetryCost-0.25) > 1e-12 {
		t.Fatalf("MeanRetryCost = %v, want 0.25", s.MeanRetryCost)
	}
	if math.Abs(s.MeanFailureCost-0.5) > 1e-12 {
		t.Fatalf("MeanFailureCost = %v, want 0.5", s.MeanFailureCost)
	}
	if math.Abs(s.MeanActionCost-0.05) > 1e-12 {
		t.Fatalf("MeanActionCost = %v, want 0.05", s.MeanActionCost)
	}
	// Weighted means apply the configured weights.
	if math.Abs(s.MeanWeightedThroughputImprovement-cfg.WImprovement*0.5) > 1e-12 {
		t.Fatalf("weighted improvement = %v, want %v", s.MeanWeightedThroughputImprovement, cfg.WImprovement*0.5)
	}
	if math.Abs(s.MeanWeightedLatencyCost-cfg.WLatency*0.5) > 1e-12 {
		t.Fatalf("weighted latency = %v, want %v", s.MeanWeightedLatencyCost, cfg.WLatency*0.5)
	}
	if math.Abs(s.MeanWeightedFailureCost-cfg.WFailure*0.5) > 1e-12 {
		t.Fatalf("weighted failure = %v, want %v", s.MeanWeightedFailureCost, cfg.WFailure*0.5)
	}
}

// buildSyntheticRun builds a LinUCB RunResult whose recorded rewards are exactly
// the reconstructed rewards, so DiagnoseRun should report zero mismatches.
func buildSyntheticRun(t *testing.T, cfg rl.RewardConfig) RunResult {
	t.Helper()
	calc, err := rl.NewRewardCalculator(cfg)
	if err != nil {
		t.Fatalf("NewRewardCalculator = %v", err)
	}
	decs := []DecisionEvent{
		{RequestIndex: 1, Action: actionKind(rl.ActionNone), ActionIndex: int(rl.ActionNone), Concurrency: 1, ChunkSize: 100, Throughput: 1000, AggregateThroughput: 1000, RequestDuration: 100 * time.Millisecond},
		{RequestIndex: 2, Action: actionKind(rl.ActionIncreaseConcurrency), ActionIndex: int(rl.ActionIncreaseConcurrency), Concurrency: 1, ChunkSize: 100, Throughput: 3000, AggregateThroughput: 3000, RequestDuration: 100 * time.Millisecond},
		{RequestIndex: 3, Action: actionKind(rl.ActionNone), ActionIndex: int(rl.ActionNone), Concurrency: 2, ChunkSize: 100, Throughput: 3000, AggregateThroughput: 3000, RequestDuration: 200 * time.Millisecond},
	}
	for i := 0; i+1 < len(decs); i++ {
		r := calc.Calculate(stateOf(decs[i]), stateOf(decs[i+1]), rl.ActionIndex(decs[i].ActionIndex), true)
		decs[i].Reward = r.Value
	}
	return RunResult{
		Mode:              ModeLinUCB,
		Err:               "",
		OutputMatches:     true,
		NoDuplicateRanges: true,
		NoMissingRanges:   true,
		Decisions:         decs,
	}
}

func TestDiagnoseRunSynthetic(t *testing.T) {
	cfg := rl.DefaultRewardConfig()
	run := buildSyntheticRun(t, cfg)
	d, err := DiagnoseRun(run, cfg)
	if err != nil {
		t.Fatalf("DiagnoseRun = %v", err)
	}
	if !d.Valid || !d.NumericallyHealthy {
		t.Fatalf("diagnostics not healthy/valid: %+v", d)
	}
	if d.Decisions != 3 {
		t.Fatalf("Decisions = %d, want 3", d.Decisions)
	}
	if d.ReconstructionMismatches != 0 {
		t.Fatalf("ReconstructionMismatches = %d, want 0", d.ReconstructionMismatches)
	}
	if got := d.Actions.Count(rl.ActionNone); got != 2 {
		t.Fatalf("ActionNone count = %d, want 2", got)
	}
	if got := d.Actions.Count(rl.ActionIncreaseConcurrency); got != 1 {
		t.Fatalf("IncreaseConcurrency count = %d, want 1", got)
	}
	// Two rewards were attributed (decisions 0 and 1).
	if d.Rewards.Count != 2 {
		t.Fatalf("reward count = %d, want 2", d.Rewards.Count)
	}

	// Determinism: the same input yields the same diagnostics.
	d2, err := DiagnoseRun(run, cfg)
	if err != nil {
		t.Fatalf("DiagnoseRun (2) = %v", err)
	}
	if !reflect.DeepEqual(d.Actions, d2.Actions) || !reflect.DeepEqual(d.Rewards, d2.Rewards) ||
		!reflect.DeepEqual(d.Components, d2.Components) || !reflect.DeepEqual(d.Churn, d2.Churn) ||
		d.CumulativeReward != d2.CumulativeReward {
		t.Fatalf("diagnostics not deterministic")
	}
}

func TestDiagnoseRunDetectsMismatch(t *testing.T) {
	cfg := rl.DefaultRewardConfig()
	run := buildSyntheticRun(t, cfg)
	// Corrupt one recorded reward so it no longer matches the reconstruction.
	run.Decisions[0].Reward += 100
	d, err := DiagnoseRun(run, cfg)
	if err != nil {
		t.Fatalf("DiagnoseRun = %v", err)
	}
	if d.ReconstructionMismatches == 0 {
		t.Fatal("expected a reconstruction mismatch to be detected")
	}
	if d.NumericallyHealthy {
		t.Fatal("run should be marked numerically unhealthy on mismatch")
	}
}

func TestCalibrationScenariosValidate(t *testing.T) {
	scenarios := CalibrationScenarios()
	if len(scenarios) != 5 {
		t.Fatalf("scenarios = %d, want 5", len(scenarios))
	}
	for _, s := range scenarios {
		if err := s.Config.Validate(); err != nil {
			t.Fatalf("scenario %q invalid: %v", s.Name, err)
		}
	}
}

func TestRunCalibrationSmall(t *testing.T) {
	cfg := DefaultScenarioConfig()
	cfg.FileSize = 32 << 10
	cfg.ChunkSize = 8 << 10
	cfg.InitialRate = 1 << 20
	cfg.ChangedRate = 1 << 20
	cfg.RateChangeAfterRequests = 0
	cfg.MinConcurrency = 1
	cfg.MaxConcurrency = 2
	cfg.MinChunkSize = 4 << 10
	cfg.MaxChunkSize = 16 << 10
	cfg.InitialChunkSize = 8 << 10
	cfg.ChunkSizeStep = 4 << 10
	cfg.MaxRetries = 2
	cfg.Timeout = 5 * time.Second
	cfg.LinUCBAlpha = 0.2
	rw := rl.DefaultRewardConfig()
	cfg.RewardConfig = &rw

	report, err := RunCalibration(CalibrationConfig{
		Scenarios:    []MatrixScenario{{Name: "small", Config: cfg}},
		Repetitions:  1,
		Alpha:        0.2,
		RewardConfig: rw,
	})
	if err != nil {
		t.Fatalf("RunCalibration = %v", err)
	}
	if len(report.Scenarios) != 1 {
		t.Fatalf("scenarios = %d, want 1", len(report.Scenarios))
	}
	sc := report.Scenarios[0]
	if len(sc.Runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(sc.Runs))
	}
	if sc.Runs[0].Decisions == 0 {
		t.Fatal("no decisions recorded")
	}
	if !sc.Runs[0].Valid || !sc.Runs[0].NumericallyHealthy {
		t.Fatalf("run not healthy/valid: %+v", sc.Runs[0])
	}
	if sc.Runs[0].ReconstructionMismatches != 0 {
		t.Fatalf("reconstruction mismatches = %d, want 0", sc.Runs[0].ReconstructionMismatches)
	}
	if sc.Pooled.Decisions != sc.Runs[0].Decisions {
		t.Fatalf("pooled decisions = %d, want %d", sc.Pooled.Decisions, sc.Runs[0].Decisions)
	}
	if !sc.Pooled.NumericallyHealthy {
		t.Fatal("pooled diagnostics not numerically healthy")
	}
}

// actionKind maps an RL action index to the existing policy.ActionKind.
func actionKind(a rl.ActionIndex) policy.ActionKind {
	action, err := rl.ActionFromIndex(a)
	if err != nil {
		panic(err)
	}
	return action.Kind
}
