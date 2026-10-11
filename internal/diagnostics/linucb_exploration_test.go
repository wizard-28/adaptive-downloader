package diagnostics

import (
	"testing"

	"adaptivedl/internal/rl"
)

func mustCalc(t *testing.T) *rl.RewardCalculator {
	t.Helper()
	c, err := rl.NewRewardCalculator(rl.DefaultRewardConfig())
	if err != nil {
		t.Fatalf("NewRewardCalculator = %v", err)
	}
	return c
}

func baselineScenario() Scenario {
	return Scenario{Name: "baseline", InitialRate: 4 << 20, ChangedRate: 4 << 20}
}

func TestRunForcedNoneCorrect(t *testing.T) {
	rr, err := runForced(baselineScenario(), rl.ActionNone, 0, nil, mustCalc(t))
	if err != nil {
		t.Fatalf("runForced = %v", err)
	}
	if rr.Err != "" {
		t.Fatalf("download error: %s", rr.Err)
	}
	if !rr.OutputMatches || !rr.NoDuplicateRanges || !rr.NoMissingRanges {
		t.Fatalf("correctness: match=%v noDup=%v noMissing=%v", rr.OutputMatches, rr.NoDuplicateRanges, rr.NoMissingRanges)
	}
	if rr.FinalConcurrency != InitConcurrency {
		t.Fatalf("ActionNone changed concurrency to %d", rr.FinalConcurrency)
	}
	if rr.FinalChunkSize != ChunkSize {
		t.Fatalf("ActionNone changed chunk size to %d", rr.FinalChunkSize)
	}
	if len(rr.Records) == 0 {
		t.Fatal("no transitions recorded")
	}
}

func TestRunForcedIncreaseConcurrencyRaisesConcurrency(t *testing.T) {
	rr, err := runForced(baselineScenario(), rl.ActionIncreaseConcurrency, 0, nil, mustCalc(t))
	if err != nil {
		t.Fatalf("runForced = %v", err)
	}
	if rr.FinalConcurrency != MaxConcurrency {
		t.Fatalf("forced increase-concurrency final = %d, want %d", rr.FinalConcurrency, MaxConcurrency)
	}
	if !rr.OutputMatches {
		t.Fatal("output mismatch under forced increase-concurrency")
	}
}

func TestRunForcedIncreaseChunkSizeRaisesChunk(t *testing.T) {
	rr, err := runForced(baselineScenario(), rl.ActionIncreaseChunkSize, 0, nil, mustCalc(t))
	if err != nil {
		t.Fatalf("runForced = %v", err)
	}
	if rr.FinalChunkSize != MaxChunkSize {
		t.Fatalf("forced increase-chunk-size final = %d, want %d", rr.FinalChunkSize, MaxChunkSize)
	}
	if !rr.OutputMatches {
		t.Fatal("output mismatch under forced increase-chunk-size")
	}
}

func TestRunBaselineReport(t *testing.T) {
	rep, err := Run(Config{
		Scenarios:   []Scenario{baselineScenario()},
		Repetitions: 1,
	})
	if err != nil {
		t.Fatalf("Run = %v", err)
	}
	if len(rep.Scenarios) != 1 || len(rep.Scenarios[0].Actions) != len(ForcedActions()) {
		t.Fatalf("unexpected report shape: %+v", rep.Scenarios)
	}
	if rep.Scenarios[0].InitState == nil {
		t.Fatal("missing initial state")
	}
	for _, a := range rep.Scenarios[0].Actions {
		if a.Decisions == 0 {
			t.Errorf("action %s produced no transitions", a.Action)
		}
	}
}
