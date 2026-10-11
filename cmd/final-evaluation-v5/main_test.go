package main

import (
	"math"
	"testing"
	"time"

	"adaptivedl/internal/experiment/server"
	"adaptivedl/internal/metrics"
	"adaptivedl/internal/rl/dqn"
)

func TestConditionsMatchEstablishedMatrix(t *testing.T) {
	cs := conditions()
	if len(cs) != 8 {
		t.Fatalf("conditions = %d, want 8 (U1-U8)", len(cs))
	}
	if cs[0].ID != "U1" || cs[7].ID != "U8" {
		t.Fatalf("condition order changed: %s..%s", cs[0].ID, cs[7].ID)
	}
	if cs[0].InitialRate != 768*1024 || cs[0].SubsequentRate != 3*1024*1024 || cs[0].TransitionAfter != 0.80 {
		t.Fatalf("U1 changed: %+v", cs[0])
	}
	if cs[2].LatencyMs != 15 || cs[2].LossPercent != 0.5 {
		t.Fatalf("U3 changed: %+v", cs[2])
	}
	if cs[7].TransitionAfter != 0.90 || cs[7].LatencyMs != 40 || cs[7].LossPercent != 1.25 {
		t.Fatalf("U8 changed: %+v", cs[7])
	}
}

func TestBuildScheduleDeterministicAndRandomized(t *testing.T) {
	cs := conditions()
	a := buildSchedule(cs, 5, 20260925)
	b := buildSchedule(cs, 5, 20260925)
	if len(a) != 40 {
		t.Fatalf("schedule = %d, want 40", len(a))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("schedule not deterministic at %d", i)
		}
	}
	firsts := map[string]int{}
	for _, p := range a {
		firsts[p.OrderFirst]++
		if p.V5RunID == "" || p.Aria2RunID == "" {
			t.Fatalf("pair %s missing run ids", p.PairID)
		}
	}
	if firsts["v5"] == 0 || firsts["aria2"] == 0 {
		t.Fatalf("order-first not randomized: %v", firsts)
	}
}

func TestAria2ArgsFromFrozenCommand(t *testing.T) {
	cmd := "aria2c --no-conf --min-split-size=1048576 --max-connection-per-server=16 --split=16 <url>"
	args := aria2ArgsFromCommand(cmd)
	for _, bad := range []string{"aria2c", "<url>"} {
		for _, a := range args {
			if a == bad {
				t.Fatalf("argv still contains %q", bad)
			}
		}
	}
	found := 0
	for _, a := range args {
		if a == "--max-connection-per-server=16" || a == "--split=16" || a == "--min-split-size=1048576" {
			found++
		}
	}
	if found != 3 {
		t.Fatalf("candidate F flags missing from argv: %v", args)
	}
}

func TestDeriveActionsAuthoritativeAfter(t *testing.T) {
	start := time.Unix(0, 0)
	times := []time.Time{start.Add(100 * time.Millisecond), start.Add(300 * time.Millisecond)}
	decisions := []dqn.Decision{
		{ActionIndex: 1, ConcurrencyBefore: 1, RemainingBytes: 15 << 20},
		{ActionIndex: 2, ConcurrencyBefore: 2, RemainingBytes: 14 << 20},
	}
	actions := deriveActions(3, decisions, times, start)
	if actions[0].ConcurrencyAfter != 2 || actions[1].ConcurrencyAfter != 3 {
		t.Fatalf("after-values = %d/%d", actions[0].ConcurrencyAfter, actions[1].ConcurrencyAfter)
	}
	if !actions[0].Changed || !actions[1].Changed {
		t.Fatal("changes not detected")
	}
}

func TestFirstReactionAfter(t *testing.T) {
	actions := []ActionEvent{
		{TimeSinceScheduleStartMs: 500, Changed: true},
		{TimeSinceScheduleStartMs: 2000, Changed: true},
		{TimeSinceScheduleStartMs: 2100, Changed: false},
	}
	fd, fc, lat := firstReactionAfter(actions, 1.0)
	if fd != 2.0 {
		t.Fatalf("first decision = %v, want 2.0s", fd)
	}
	if fc != 2.0 || math.Abs(lat-1000) > 1e-9 {
		t.Fatalf("first change = %v latency = %v, want 2.0s/1000ms", fc, lat)
	}
}

func TestValidationHelpers(t *testing.T) {
	good := []metrics.Record{
		{RangeStart: 0, RangeEnd: 9, Bytes: 10, Success: true},
		{RangeStart: 10, RangeEnd: 19, Bytes: 10, Success: true},
	}
	if !validateRangeTiling(good, 20) {
		t.Fatal("valid tiling rejected")
	}
	decisions := []dqn.Decision{{RemainingBytes: 10}, {RemainingBytes: 0}}
	mono, within, matches := checkProgress(decisions, good, 20)
	if !mono || !within || !matches {
		t.Fatalf("valid progress rejected: %v %v %v", mono, within, matches)
	}
}

func TestInFlightStats(t *testing.T) {
	base := time.Unix(0, 0)
	ev := func(s, e int64) server.RequestEvent {
		return server.RequestEvent{
			Start: base.Add(time.Duration(s) * time.Second),
			End:   base.Add(time.Duration(e) * time.Second),
		}
	}
	max, mean := inFlightStats([]server.RequestEvent{ev(0, 10), ev(1, 3), ev(2, 4)})
	if max != 3 {
		t.Fatalf("max = %d, want 3", max)
	}
	// Timeline: [0,1)=1, [1,2)=2, [2,3)=3, [3,4)=2, [4,10)=1 => 14/10 = 1.4.
	if math.Abs(mean-1.4) > 1e-9 {
		t.Fatalf("mean = %v, want 1.4", mean)
	}
}

func TestContentPatternHashStable(t *testing.T) {
	a := sha256Bytes(contentPattern(1024))
	b := sha256Bytes(contentPattern(1024))
	if a != b {
		t.Fatal("content hash not stable")
	}
}
