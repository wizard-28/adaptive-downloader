package main

import (
	"math"
	"testing"
	"time"

	"adaptivedl/internal/experiment/server"
	"adaptivedl/internal/metrics"
	"adaptivedl/internal/rl/dqn"
)

func testDecision(idx, before int, effective, noop bool, remaining int64) dqn.Decision {
	return dqn.Decision{
		ActionIndex: idx, ConcurrencyBefore: before,
		Effective: effective, NoOp: noop, RemainingBytes: remaining,
	}
}

func TestDeriveActionsAuthoritativeAfter(t *testing.T) {
	start := time.Unix(0, 0)
	times := []time.Time{
		start.Add(100 * time.Millisecond),
		start.Add(300 * time.Millisecond),
		start.Add(600 * time.Millisecond),
	}
	decisions := []dqn.Decision{
		testDecision(1, 1, true, false, 3<<20),
		testDecision(1, 2, true, false, 2<<20),
		testDecision(2, 3, true, false, 1<<20),
	}
	actions := deriveActions(2, decisions, times, start)
	if len(actions) != 3 {
		t.Fatalf("actions = %d, want 3", len(actions))
	}
	wantAfter := []int{2, 3, 2}
	for i, a := range actions {
		if a.ConcurrencyAfter != wantAfter[i] {
			t.Fatalf("action %d after = %d, want %d", i, a.ConcurrencyAfter, wantAfter[i])
		}
		if !a.Changed {
			t.Fatalf("action %d should be a change", i)
		}
	}
	if actions[0].TimeSinceScheduleStartMs != 100 {
		t.Fatalf("timestamp = %v, want 100ms", actions[0].TimeSinceScheduleStartMs)
	}
	// The field must come from the next observed controller target, not from
	// the model's pre-application Decision.ConcurrencyAfter.
	if actions[0].ConcurrencyAfter != decisions[1].ConcurrencyBefore {
		t.Fatal("after-value is not the next authoritative controller target")
	}
}

func TestValidateRangeTiling(t *testing.T) {
	good := []metrics.Record{
		{RangeStart: 0, RangeEnd: 9, Bytes: 10, Success: true},
		{RangeStart: 10, RangeEnd: 19, Bytes: 10, Success: true},
	}
	if !validateRangeTiling(good, 20) {
		t.Fatal("valid tiling rejected")
	}
	gap := []metrics.Record{
		{RangeStart: 0, RangeEnd: 9, Bytes: 10, Success: true},
		{RangeStart: 11, RangeEnd: 19, Bytes: 9, Success: true},
	}
	if validateRangeTiling(gap, 20) {
		t.Fatal("gap accepted")
	}
	overlap := []metrics.Record{
		{RangeStart: 0, RangeEnd: 9, Bytes: 10, Success: true},
		{RangeStart: 9, RangeEnd: 19, Bytes: 11, Success: true},
	}
	if validateRangeTiling(overlap, 20) {
		t.Fatal("overlap accepted")
	}
	withFailed := append(append([]metrics.Record(nil), good...),
		metrics.Record{RangeStart: 0, RangeEnd: 9, Bytes: 10, Success: false})
	if !validateRangeTiling(withFailed, 20) {
		t.Fatal("failed retry record should not break tiling")
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
	events := []server.RequestEvent{ev(0, 10), ev(1, 3), ev(2, 4)}
	max, meanInFlight := inFlightStats(events)
	if max != 3 {
		t.Fatalf("max in flight = %d, want 3", max)
	}
	// Timeline: [0,1)=1, [1,2)=2, [2,3)=3, [3,4)=2, [4,10)=1 => 14/10 = 1.4.
	if math.Abs(meanInFlight-1.4) > 1e-9 {
		t.Fatalf("mean in flight = %v, want 1.4", meanInFlight)
	}
}

func TestCheckProgress(t *testing.T) {
	records := []metrics.Record{
		{RangeStart: 0, RangeEnd: 0, Bytes: 1, Success: true},
		{RangeStart: 1, RangeEnd: 1, Bytes: 1, Success: true},
	}
	decisions := []dqn.Decision{
		{RemainingBytes: 1},
		{RemainingBytes: 0},
	}
	mono, within, matches, _ := checkProgress(decisions, records, 2)
	if !mono || !within || !matches {
		t.Fatalf("valid progress rejected: mono=%v within=%v matches=%v", mono, within, matches)
	}
	bad := []dqn.Decision{{RemainingBytes: 1}, {RemainingBytes: 1}}
	if _, _, matches, _ := checkProgress(bad, records, 2); matches {
		t.Fatal("fabricated progress accepted")
	}
	if _, _, matches, _ := checkProgress(decisions[:1], records, 2); matches {
		t.Fatal("decision/record length mismatch accepted")
	}
}

func TestClassifyTrajectory(t *testing.T) {
	a := func(before, after int) ActionEvent {
		return ActionEvent{ConcurrencyBefore: before, ConcurrencyAfter: after, Changed: before != after}
	}
	constant, ramp, mono := classifyTrajectory([]ActionEvent{a(3, 3), a(3, 3)}, 4)
	if !constant || ramp || mono {
		t.Fatalf("constant misclassified: %v %v %v", constant, ramp, mono)
	}
	constant, ramp, mono = classifyTrajectory([]ActionEvent{a(1, 2), a(2, 3), a(3, 4)}, 4)
	if constant || !ramp || mono {
		t.Fatalf("ramp misclassified: %v %v %v", constant, ramp, mono)
	}
	constant, ramp, mono = classifyTrajectory([]ActionEvent{a(4, 3), a(3, 2)}, 4)
	if constant || ramp || !mono {
		t.Fatalf("decrease misclassified: %v %v %v", constant, ramp, mono)
	}
	constant, ramp, mono = classifyTrajectory([]ActionEvent{a(1, 2), a(2, 1)}, 4)
	if constant || ramp || mono {
		t.Fatalf("mixed trajectory misclassified: %v %v %v", constant, ramp, mono)
	}
}

func TestAnalyzeReaction(t *testing.T) {
	actions := []ActionEvent{
		{TimeSinceScheduleStartMs: 500, ConcurrencyBefore: 1, ConcurrencyAfter: 2, Changed: true},
		{TimeSinceScheduleStartMs: 2000, ConcurrencyBefore: 2, ConcurrencyAfter: 3, Changed: true},
	}
	r := analyzeReaction(1, actions, 3000, 1000, 4)
	if r.AtTransitionConcurrency != 2 {
		t.Fatalf("at-transition concurrency = %d, want 2", r.AtTransitionConcurrency)
	}
	if r.FirstDecisionAfter == nil || r.FirstDecisionAfter.TimeSinceScheduleStartMs != 2000 {
		t.Fatalf("first decision after transition = %+v", r.FirstDecisionAfter)
	}
	if !r.Reacted || r.ReactionLatencyMs != 1000 {
		t.Fatalf("reaction = %+v", r)
	}
	if r.PreChanges != 1 || r.PostChanges != 1 {
		t.Fatalf("changes pre/post = %d/%d, want 1/1", r.PreChanges, r.PostChanges)
	}
	if math.Abs(r.PreMeanConcurrency-1.5) > 1e-9 {
		t.Fatalf("pre mean = %v, want 1.5", r.PreMeanConcurrency)
	}
	if math.Abs(r.PostMeanConcurrency-2.5) > 1e-9 {
		t.Fatalf("post mean = %v, want 2.5", r.PostMeanConcurrency)
	}
}

func TestConcurrencySegmentsDominant(t *testing.T) {
	actions := []ActionEvent{
		{TimeSinceScheduleStartMs: 1000, ConcurrencyBefore: 1, ConcurrencyAfter: 2, Changed: true},
	}
	seg := concurrencySegments(1, actions, 3000, 4)
	dc, frac := dominantConcurrency(seg)
	if dc != 2 {
		t.Fatalf("dominant = C%d, want C2", dc)
	}
	if math.Abs(frac-2.0/3.0) > 1e-9 {
		t.Fatalf("dominant fraction = %v, want 2/3", frac)
	}
}
