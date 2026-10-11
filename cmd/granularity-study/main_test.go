package main

import (
	"testing"
	"time"

	"adaptivedl/internal/experiment/network"
	"adaptivedl/internal/experiment/server"
)

func TestSummarizeRanges(t *testing.T) {
	rs := []server.Range{
		{Start: 0, End: 1048575},
		{Start: 1048576, End: 2097151},
		{Start: 2097152, End: 3145727},
		{Start: 3145728, End: 4194303},
	}
	s := summarizeRanges(rs)
	if s.Count != 4 || s.Min != 1048576 || s.Max != 1048576 || s.Mean != 1048576 {
		t.Fatalf("summary = %+v", s)
	}
}

func TestContentPatternDeterministic(t *testing.T) {
	if string(contentPattern(128)) != string(contentPattern(128)) {
		t.Fatal("contentPattern not deterministic")
	}
}

// TestGranularityRunBaseline runs one matched-granularity baseline download and
// checks correctness and the observed 1 MiB request split. It does not use
// network shaping.
func TestGranularityRunBaseline(t *testing.T) {
	sc := Scenario{Name: "baseline", InitialRate: 4 << 20, ChangedRate: 4 << 20}
	content := contentPattern(fileSize)
	rr := runOne(sc, content, 0, network.NewController())
	if rr.Err != "" {
		t.Fatalf("runOne error: %s", rr.Err)
	}
	if !rr.OutputMatches {
		t.Fatal("output mismatch")
	}
	if rr.TotalAttempts != 4 || rr.Range.Count != 4 {
		t.Fatalf("attempts=%d ranges=%d, want 4/4", rr.TotalAttempts, rr.Range.Count)
	}
	if rr.Range.Min != chunkSize || rr.Range.Max != chunkSize {
		t.Fatalf("range sizes = %d..%d, want %d", rr.Range.Min, rr.Range.Max, chunkSize)
	}
	if rr.MaxInFlight < 1 || rr.MaxInFlight > concurrency {
		t.Fatalf("max in-flight = %d, want 1..%d", rr.MaxInFlight, concurrency)
	}
}

// TestWallclockScenarioMatchedGranularity verifies the extended command's
// wall-clock scenario produces 4x1MiB requests with max in-flight 4 and that
// all requests start before the 1s transition.
func TestWallclockScenarioMatchedGranularity(t *testing.T) {
	sc := wallclockScenario()
	if sc.ScheduleTransitionAfter != time.Second || sc.ScheduleSubsequentRate != 4<<20 || sc.InitialRate != 512<<10 {
		t.Fatalf("wallclockScenario config = %+v", sc)
	}
	content := contentPattern(fileSize)
	rr := runOne(sc, content, 0, network.NewController())
	if rr.Err != "" {
		t.Fatalf("runOne error: %s", rr.Err)
	}
	if !rr.OutputMatches || rr.TotalAttempts != 4 || rr.Range.Count != 4 {
		t.Fatalf("match=%v attempts=%d ranges=%d", rr.OutputMatches, rr.TotalAttempts, rr.Range.Count)
	}
	if rr.Range.Min != chunkSize || rr.Range.Max != chunkSize {
		t.Fatalf("range sizes = %d..%d, want %d", rr.Range.Min, rr.Range.Max, chunkSize)
	}
	if rr.MaxInFlight != 4 {
		t.Fatalf("max in-flight = %d, want 4", rr.MaxInFlight)
	}
	if rr.NonRangeReqs != 0 {
		t.Fatalf("non-range requests = %d, want 0", rr.NonRangeReqs)
	}
	if rr.RequestsPre1 != 4 || rr.RequestsPost1 != 0 {
		t.Fatalf("pre/post = %d/%d, want 4/0 (1 MiB requests complete before 1s transition)", rr.RequestsPre1, rr.RequestsPost1)
	}
}
