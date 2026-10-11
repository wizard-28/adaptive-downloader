package experiment

import (
	"testing"
	"time"
)

// TestWallClockScheduleScenario runs one fixed-mode run of the experimental
// wall-clock throttle scenario and checks correctness and that the server-side
// transition is time-based (request log shows the rate change at ~1s elapsed).
func TestWallClockScheduleScenario(t *testing.T) {
	cfg := DefaultScenarioConfig()
	cfg.FileSize = 256 << 10
	cfg.ChunkSize = 32 << 10
	cfg.MinChunkSize = 32 << 10
	cfg.MaxChunkSize = 32 << 10
	cfg.InitialChunkSize = 32 << 10
	cfg.ChunkSizeStep = 32 << 10
	cfg.ScheduleInitialRate = 128 << 10
	cfg.ScheduleTransitionAfter = 50 * time.Millisecond
	cfg.ScheduleSubsequentRate = 4 << 20
	cfg.EnableRequestLog = true
	cfg.Repetitions = 1

	res, err := RunSelected(cfg, []Mode{ModeFixed})
	if err != nil {
		t.Fatalf("RunSelected = %v", err)
	}
	runs := res.Runs(ModeFixed)
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(runs))
	}
	if !runs[0].OutputMatches || !runs[0].NoDuplicateRanges || !runs[0].NoMissingRanges || runs[0].Err != "" {
		t.Fatalf("run not correct: %+v", runs[0])
	}
	log := runs[0].RequestLog
	if len(log) == 0 {
		t.Fatal("no request log recorded")
	}
	var firstSub int = -1
	base := log[0].Start
	for i, e := range log {
		if e.Rate == 4<<20 {
			firstSub = i
			break
		}
	}
	if firstSub < 0 {
		t.Fatal("no post-transition request recorded")
	}
	elapsed := log[firstSub].Start.Sub(base)
	if elapsed < cfg.ScheduleTransitionAfter {
		t.Fatalf("first post-transition request at %v, want >= %v", elapsed, cfg.ScheduleTransitionAfter)
	}
	// Every request before the first post-transition request must be at the
	// initial rate (time-based, not count-based).
	for i := 0; i < firstSub; i++ {
		if log[i].Rate != cfg.ScheduleInitialRate {
			t.Fatalf("request %d rate = %d, want initial %d", i, log[i].Rate, cfg.ScheduleInitialRate)
		}
	}
}

// TestWallClockScheduleValidation checks the schedule config validation.
func TestWallClockScheduleValidation(t *testing.T) {
	cases := map[string]func(*ScenarioConfig){
		"negative transition": func(c *ScenarioConfig) { c.ScheduleTransitionAfter = -time.Second },
		"missing subsequent":  func(c *ScenarioConfig) { c.ScheduleSubsequentRate = 0 },
		"missing transition":  func(c *ScenarioConfig) { c.ScheduleTransitionAfter = 0 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := DefaultScenarioConfig()
			cfg.ScheduleInitialRate = 512 << 10
			cfg.ScheduleSubsequentRate = 4 << 20
			cfg.ScheduleTransitionAfter = time.Second
			mutate(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("Validate() = nil, want error")
			}
		})
	}
}
